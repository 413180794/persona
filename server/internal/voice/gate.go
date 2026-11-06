package voice

import "time"

// The user-speech gate (docs/voice.md "Sending, the gate and 409"): what the voice model hands over is only done (sent to a
// conversation, sent to an agent) when the user has spoken since the model last took in an agent's words, and lately.
// An agent's output reaches the model (a result read out); were it to say "now send X" and the model did, the agent
// would be driving itself through the user's voice. Reading (a status, the knowledge base) is not gated.
//
// What counts as the user speaking: a caption of theirs (any part of one), or a line they typed. Captions come late on
// some services: the measured one sends the user's words 1.2–1.4 s after they stop, after the model has begun to answer
// and called its tool (docs/lessons/voice.md "Timeline"), so a handover that does not pass waits gateWait for them first.
//
// When the model took the agent's words in: the measured service holds a commentary sent while the model speaks until
// that speech is over (26–40 s and more, lessons row 3) and then acks it (session.commentary.appended, evAck). Words
// sent close the gate at once (for a service that does not ack, or takes them in at once); their ack closes it again,
// so what the user said while they waited does not count for what the model does after reading them. A handover is held
// to the gate as it was when the model made it (the agent time then): words it had not taken in yet cannot have driven
// it.

const gateWindow = 30 * time.Second // the user's last words are at most this old

// gateWait is how long a handover waits for the caption of what the user just said.
var gateWait = 2500 * time.Millisecond

// speechGate is a conversation's gate (under conversation.mu).
type speechGate struct {
	user  time.Time // the user's last words
	agent time.Time // the last time an agent's words were sent to the model, or it took them in
}

// told: an agent's words were sent, or taken in, at now.
func (g *speechGate) told(now time.Time) {
	if now.After(g.agent) {
		g.agent = now
	}
}

// openSince: the user has spoken since agent (the gate's agent time when the model acted), within gateWindow of now.
func (g *speechGate) openSince(agent, now time.Time) bool {
	return !g.user.IsZero() && g.user.After(agent) && now.Sub(g.user) <= gateWindow
}

// The insertion gate (docs/voice.md "Context for the fast layer" rule 3, its times from docs/lessons/voice.md "Suggested constants"): what
// the extension says to the model by itself (a say: a result whose call is gone, a card waiting on the user) waits
// until
//   - the user has been quiet for userQuiet, by the energy of the app's audio: their caption comes 1.2–1.4 s after
//     they stop, whole, too late to tell; a say sent while they talk is swallowed (lessons row 3);
//   - the model's own speech has played out for speechQuiet: its play end is added up from the audio's length, since
//     the audio comes 9–10 times faster than it plays;
//   - the model's last handover is delegationQuiet old: the reply it hands over in is its acknowledgement (4–5 s, no
//     separate one on the measured service), and the play end of that reply is not known before its audio is in.
// A delegation's own answer (its one commentary) waits for the user to be quiet and for the model's speech to have
// played out (no margin): the measured service holds a commentary until that speech is over by itself (lessons row 3,
// specific to it), but a standard upstream may let one cut the model off, and voice mode only counts on the standard.
// An answer ready at once (a status: 0.1–0.3 s) would beat the audio of the reply the model handed over in (its call
// comes 0.1–0.2 s into that reply), so it also waits replyAudioIn after the model's last handover: by then that audio
// is in (it comes 9–10 times faster than it plays) and its play end is known. On the measured service none of this
// costs time: it holds the answer that long anyway. A tool's result on its call is not held.
//
// The user's voice holds nothing for ever: the app's level can stay over voiceRMS with nobody talking to the model (a
// TV, a car, people nearby, echo residue; lessons "Suggested constants" asks for real devices), so a frame has a voice in it
// only when it is also well over the quietest the audio has been lately (noiseFloor), and past answerHoldMost (an
// answer) or sayHoldMost (a say) from when it was queued the user's voice no longer holds it. The model's speech still
// does: that always ends.

var (
	userQuiet       = 1200 * time.Millisecond
	speechQuiet     = 1500 * time.Millisecond
	delegationQuiet = 3 * time.Second
	replyAudioIn    = time.Second
	// captionQuiet: an open caption of the user's with no more of it this long is their line done (the services send
	// no end of a user's line).
	captionQuiet = 1200 * time.Millisecond
	// answerHoldMost and sayHoldMost: the longest the user's voice holds back a delegation's answer (the model and the
	// user wait for it) and a say.
	answerHoldMost = 4 * time.Second
	sayHoldMost    = 15 * time.Second
)

// voiceRMS is the level of a frame of the app's audio that has the user's voice in it (about -40 dBFS, lessons
// "Timeline"); a muted app sends zeros.
const voiceRMS = 300

const (
	// floorSeconds is how far back the noise floor looks.
	floorSeconds = 6
	// floorMargin is how many times the floor's level a frame needs to have a voice in it (about 6 dB).
	floorMargin = 2
)

// pacing is what the insertion gate goes by (under conversation.mu).
type pacing struct {
	voice     time.Time // the app's audio last had the user's voice in it
	playEnd   time.Time // when what the model said so far has played out
	delegated time.Time // the model last handed something over (a delegation, a function call)
	floor     noiseFloor
}

// played adds a piece of the model's audio, n bytes of PCM16 at rate, arrived at now.
func (p *pacing) played(now time.Time, n, rate int) {
	if rate <= 0 || n <= 0 {
		return
	}
	if p.playEnd.Before(now) {
		p.playEnd = now
	}
	p.playEnd = p.playEnd.Add(time.Duration(n/2) * time.Second / time.Duration(rate))
}

// heard takes a frame of the app's audio, of mean square ms, arrived at now: whether it has the user's voice in it
// (over voiceRMS, and floorMargin times over the noise floor).
func (p *pacing) heard(now time.Time, ms float64) bool {
	floor := p.floor.add(now, ms)
	if ms <= voiceRMS*voiceRMS || ms <= floorMargin*floorMargin*floor {
		return false
	}
	p.voice = now
	return true
}

// readyAt is when something queued at queued may go: said (a say), or as a delegation's answer.
func (p *pacing) readyAt(say bool, queued time.Time) time.Time {
	most := answerHoldMost
	if say {
		most = sayHoldMost
	}
	user := p.voice.Add(userQuiet)
	if limit := queued.Add(most); user.After(limit) {
		user = limit
	}
	if !say {
		return latest(user, p.playEnd, p.delegated.Add(replyAudioIn))
	}
	return latest(user, p.playEnd.Add(speechQuiet), p.delegated.Add(delegationQuiet))
}

func latest(ts ...time.Time) time.Time {
	var out time.Time
	for _, t := range ts {
		if t.After(out) {
			out = t
		}
	}
	return out
}

// noiseFloor is the app's audio at its quietest lately: the lowest frame of each of the last floorSeconds seconds.
// Speech has gaps between its words and phrases that fall to the room's level; a steady noise (a TV, a car, echo
// residue) does not fall below its own.
type noiseFloor struct {
	sec [floorSeconds]int64   // which second (Unix) each slot is of
	low [floorSeconds]float64 // the lowest mean square of a frame in it
}

// add takes a frame of mean square ms at now and returns the floor's level (a mean square): the lowest of the slots,
// 0 until each of the last floorSeconds seconds had a frame (a call just begun, a gap in the audio).
func (f *noiseFloor) add(now time.Time, ms float64) float64 {
	s := now.Unix()
	i := int(s % floorSeconds)
	if f.sec[i] != s {
		f.sec[i], f.low[i] = s, ms
	} else if ms < f.low[i] {
		f.low[i] = ms
	}
	level := f.low[i]
	for k := range floorSeconds {
		if s-f.sec[k] >= floorSeconds {
			return 0
		}
		level = min(level, f.low[k])
	}
	return level
}

// meanSquare is the mean square of a frame of PCM16 (little-endian).
func meanSquare(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(uint16(pcm[2*i]) | uint16(pcm[2*i+1])<<8))
		sum += v * v
	}
	return sum / float64(n)
}

// Barging in (docs/voice.md "Context for the fast layer" rule 7, its numbers from docs/lessons/voice.md "Timeline" and "Suggested constants",
// protocol 2 only): the user talking over the model drops what it has not played yet (clear), as the upstream says. The
// measured service stops the model by itself when the user cuts in for real (0.64 s and longer stopped it, a 0.43 s
// "mm-hm" did not) and says so (response.incomplete): clear follows that, at once. An upstream that never says when its
// replies start, end or are cut off gets the energy rule instead: the app's audio has had the user's voice in it for
// bargeMin in a row (gaps between words up to bargeGap) while the model's speech still plays (its computed play end).
// Not by the caption, nor a list of backchannel words: the caption comes 1.2 s after the user stops, too late for
// 0.5 s; and not the model's own 0.8 s to stop, which misses the 0.64–0.70 s cut-ins it stops for, while its abandoned
// answer is mostly in the app already (it comes 9–10 times faster than it plays). A long backchannel ("yeah yeah yeah",
// 0.7 s) cuts the answer too, as it does the measured model's.
//
// What the model says next, its reply to the user, is not dropped with the rest: its audio comes 1.2–1.3 s after they
// stop (its reply begins 0.9 s after), before the 1.5 s pause that ends a drop otherwise and often before their
// caption (lessons "Timeline"). Audio once they have been quiet replyAfterVoice, after a pause of cutGap in what came (the
// rest of the dropped answer comes in one burst, 9–10 times faster than it plays), is that reply.
var (
	bargeMin        = 500 * time.Millisecond
	bargeGap        = 120 * time.Millisecond
	replyAfterVoice = 700 * time.Millisecond
	cutGap          = 400 * time.Millisecond
)

// barge is the user's voice in the app's audio as the energy rule counts it: a stretch of it, by the audio's own
// length (frames come in bursts over the network), not by when they arrived.
type barge struct {
	voiced time.Duration // the stretch so far, its short gaps included
	gap    time.Duration // the audio without a voice since its last voiced frame
}

// add takes a frame of length d, voiced or not, and returns the stretch the user has been talking for.
func (b *barge) add(voiced bool, d time.Duration) time.Duration {
	switch {
	case voiced:
		b.voiced += b.gap + d
		b.gap = 0
	case b.voiced > 0:
		if b.gap += d; b.gap > bargeGap {
			*b = barge{}
		}
	}
	return b.voiced
}

// prioHeartbeat is a heartbeat's place in what waits for the insertion gate (sentWords.prio, then oldest first): after
// everything else (results are 0; a card waiting on the user, #3504, goes before them), and dropped once it is stale.
const prioHeartbeat = -1
