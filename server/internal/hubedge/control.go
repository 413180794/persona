package hubedge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// ErrNoEdge: nothing answers on the control socket (the hub runs without an edge, or is not running).
var ErrNoEdge = errors.New("hubedge: no edge answers")

func call(control string, m Msg, within time.Duration, each func(Msg)) (Msg, error) {
	c, err := net.DialTimeout("unix", control, 3*time.Second)
	if err != nil {
		return Msg{}, fmt.Errorf("%w: %v", ErrNoEdge, err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(within))
	if err := (&lineWriter{w: c}).send(m); err != nil {
		return Msg{}, err
	}
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var r Msg
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.T == MsgProgress {
			if each != nil {
				each(r)
			}
			continue
		}
		return r, nil
	}
	if err := sc.Err(); err != nil {
		return Msg{}, err
	}
	return Msg{}, errors.New("hubedge: the edge closed the connection without an answer")
}

// Status asks the edge on control what serves now.
func Status(control string) (Msg, error) {
	return call(control, Msg{T: MsgStatus}, 10*time.Second, nil)
}

// Switch asks the edge on control to switch to binary (prev: what to go back to if it fails while taking over),
// calling progress with each phase; it returns once the switch is over, with the slot serving then, or after within
// (the switch goes on in the edge).
func Switch(control, binary, prev string, within time.Duration, progress func(phase string)) (Msg, error) {
	r, err := call(control, Msg{T: MsgSwitch, Binary: binary, Prev: prev, Kind: KindDeploy}, within, func(m Msg) {
		if progress != nil {
			progress(m.Phase)
		}
	})
	if err != nil {
		return r, err
	}
	if !r.OK {
		return r, errors.New(r.Reason)
	}
	return r, nil
}
