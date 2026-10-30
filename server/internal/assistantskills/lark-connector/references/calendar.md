# Calendar: events, free-busy, meeting rooms, calendars

Paths are relative to `/agent/v1`.

The schedule reminders UI groups existing calendar events by today and tomorrow; this presentation change does not alter agent APIs.

- ⚠ means other people see the change, or it cannot be undone. Confirm first.
- _(unverified)_ means the code says this was never run against the real Lark gateway. Read the result back.
- Meetings and minutes are in `meetings.md`.

## Contents

1. Ids and time
2. Read events
3. Create, change, delete
4. RSVP, share, other event actions
5. Free-busy and working hours
6. Meeting rooms
7. Calendars
8. Event cards in chats
9. Workflows

## 1. Ids and time

- **Event ids:**
  - Use `sourceId` from `/calendar/week`, or `id` from `/calendar/search`.
  - In the week view, an occurrence of a recurring event has `id = "sourceId:startSeconds"`. Passing that id to any `/calendar/event/:id` route fails with `Gateway HTTP 400 for WEB_MGET_EVENTS_WITH_IDS`.
  - The `blocks[].id` values from free-busy are not event ids either.
- **Occurrences:** to address one occurrence, pass `instanceStartAt` set to that occurrence's `startAt`, together with `span`.
- **Times:**
  - Write ISO strings **with an offset** (`2026-10-20T15:00:00+08:00`). Responses come back in UTC (`Z`).
  - Search and attachment `instanceStartAt` reject times without an offset. Other routes treat them as server-local time.
- **Calendar ids:** create events on the calendar that is both `isPrimary` and `canWrite` in `GET /calendar/calendars`. Subscribed and shared calendars are usually read-only, so often only one qualifies.
- **Room ids:** a room is addressed by `rooms[].calendarId`, not `rooms[].id`.
- **Group ids:** the group `chatId` is the feedId.

## 2. Read events

| Call | Notes |
| --- | --- |
| `GET /calendar/week?start=&end=` | Default range is the next 7 days, max 62 days. Each event has `id`, `sourceId`, `calendarId`, `startAt`, `endAt`, `rrule`, `selfAttendeeStatus` (1 pending, 2 accept, 3 tentative, 4 decline, 5 removed), `title` and `isFree`. Only the primary calendar and calendars visible in this client are included. The header `x-lark-connector-partial: 1` means some calendars failed to load. A range that had already ended when its local copy was taken, and was copied within 24h, answers from that copy with no upstream read and carries `x-lark-connector-cache: stored`; any calendar write (other than the visibility toggle, which rewrites the copies) or change push drops those copies, so the next read is live |
| `GET /calendar/event/SOURCE_ID` | Returns `canModify`, `canTransfer`, `isRecurring`, `attendees[]` (`attendeeCalendarId`, `category` 1 person / 2 group / 3 room / 4 email, `status`), `attendeeSummary`, `meetingUrl`, `larkMeeting.meetingNumber`, `attachments`, `visibility` and `reminderMinutes`. **`attendees` can be incomplete**, so use `/guests` for the full list. A series master shows the first occurrence's time |
| `GET /calendar/search?q=&calendarIds=&attendeeUserIds=&start=&end=` | `q` is 1–200 characters. `calendarIds` takes up to 20 and `attendeeUserIds` up to 10. `start` and `end` need an offset. No paging. `items[].id` works with the detail route |
| `GET /calendar/event/ID/guests?cursor=` | 50 per page. The first page also lists invited groups (`groups[].chatId`) |
| `GET /calendar/event/ID/guests/groups/CHAT_ID` | Members of one invited group |
| `GET /calendar/event/ID/share-link` | Returns `{url,state}`. Anyone with the link can see the event and ask to join |
| `GET /calendar/event/ID/attachments/TOKEN/download?name=` | Downloads an attachment listed on the event |

## 3. Create, change, delete

⚠ **Create** with `POST /calendar/events`.

- Required fields: `calendarId`, `title` (1–500), `startAt`, `endAt`.
- Optional fields:
  - Time: `timezone` (**always send it**; without it the event later refuses to create a meeting group), `endTimezone`, `allDay`, `isFree`.
  - Text: `description` (≤4000), `location` (≤500).
  - Recurrence and reminders: `reminderMinutes` (≤5 values, −1440..40320; for all-day events a negative value means a time of day, e.g. −480 is 08:00), `rrule` (`FREQ=DAILY|WEEKLY|MONTHLY|YEARLY;…`).
  - Display: `visibility` (default, public, private), `color`.
  - Guests: `attendeeUserIds` (≤50), `roomCalendarIds` (≤5), `attendeeGroupIds` (≤20 feedIds), ⚠ `attendeeEmails` (≤50, mails outside people; _(unverified)_), `guestCanInvite`, `guestCanModify`, `guestCanSeeOtherGuests`.
- The schema is **not strict**, so a misspelled field is dropped silently.
- The response is `{calendarId,created,startAt,endAt,title}` and has **no event id**. To get the id, read the week back and match on title and start.

⚠ **Change** with `PATCH /calendar/event/SOURCE_ID`.

- The schema is **strict**: unknown fields return 400.
- It takes the create fields, plus:
  - Add guests: `addAttendeeUserIds`, `addRoomCalendarIds` (≤5), `addAttendeeGroupIds`, `addAttendeeEmails`.
  - Remove guests or rooms: `removeAttendeeCalendarIds` (≤100, the `attendeeCalendarId` values). Only the organizer can release a room; others get 403 `CALENDAR_EDIT_FORBIDDEN`.
  - Move to another calendar: `calendarId` (organizer only; for a series it needs `span:"all"`).
  - Scope: `span` (`this`, `future`, `all`) and `instanceStartAt`.
  - Notifications: `notifyAttendees:false` skips them (the effect is unverified).
- **A recurring event needs `span`**, otherwise you get 400 `INVALID_CALENDAR_EVENT_SCOPE`. `this` and `future` also need `instanceStartAt`. `span:"this"` cannot change `rrule`.
- With `span:"all"`, a time change shifts the whole series by the same offset.
- A body that contains only span, instanceStartAt or notifyAttendees returns 400.
- 400 `CALENDAR_EVENT_RELOAD_REJECTED` can be retried.
- Attendees are notified by default.

⚠ **Delete** with `DELETE /calendar/event/SOURCE_ID?span=this|future|all&instanceStartAt=`. Recurring events need both query parameters. It cannot be undone.

## 4. RSVP, share, other event actions

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ RSVP | `POST /calendar/event/ID/rsvp {reply:"accept"\|"decline"\|"tentative"}` | On a series master, the answer applies to the whole series |
| ⚠ RSVP with a note | `POST /calendar/event/ID/rsvp-comment {reply,comment≤500}` | The note goes to the organizer's chat |
| ⚠ Share to chats | `POST /calendar/event/ID/share {feedIds:[1–50]}` | Posts an event card. Returns `failedFeedIds` |
| ⚠ Transfer to a new organizer | `POST /calendar/event/ID/transfer {userId,keepSelf?}` | Organizer only, and cannot be undone. _(unverified)_ |
| ⚠ Create a meeting group | `POST /calendar/event/ID/meeting-group` | Returns `{chatId}`. 409 `EVENT_MEETING_GROUP_REFUSED` if the event has no timezone. _(unverified)_ |
| ⚠ Create meeting notes | `POST /calendar/event/ID/meeting-notes` | Returns `{url}`, reusing the doc if one exists. Needs `canModify` |
| Attachments | `POST /calendar/event/ID/attachments --upload FILE --url-query name=… [--url-query span=… --url-query instanceStartAt=…]` (≤20 MB), `DELETE /calendar/event/ID/attachments/TOKEN?span=&instanceStartAt=` | Needs `canModify` |
| Copy guests for a new event | `GET /calendar/event/ID/copy-guests` |  |
| Close a reminder | `POST /calendar/reminders/close {reminderId,startAt}` | `reminderId` comes from the `calendar.reminder` event |

## 5. Free-busy and working hours

| Call | Notes |
| --- | --- |
| `GET /calendar/free-busy?userIds=a,b&start=&end=` | 1–20 people, at most 14 days. Returns `people[{userId,accessible,blocks[{startAt,endAt,allDay,title?}]}]`. `accessible:false` means the person does not share free-busy. **Do not call it in parallel**: Lark rate-limits it (`FreqLimitReachedErr`) |
| `GET /calendar/people/USER_ID/busy?start=&end=` | One person, at most 62 days |
| `POST /calendar/work-hours {userIds≤50, calendarIds≤100}` | Read-only. Returns `people[{workHours.days,timezone}]` (minutes from midnight) |
| `GET /calendar/schedulers`, `GET /calendar/schedulers/ID/availability?start=&end=` | Your booking pages and their open slots (≤31 days). Booking through them is not supported |

## 6. Meeting rooms

To book a room, add it as a guest:

- New event: `roomCalendarIds` on create.
- Existing event: `addRoomCalendarIds` on PATCH.

| Call | Notes |
| --- | --- |
| `GET /calendar/meeting-rooms/buildings` | Returns `buildings[{id,name,floors[]}]`, possibly hundreds. Filter by name yourself |
| `GET /calendar/meeting-rooms/buildings/BUILDING/availability?start=&end=&floor=F3&timezone=Asia/Shanghai` | **One floor per call**, at most 1 day. Leaving out `floor` gives the first floor with rooms; `floorsWithRooms[]` lists the rest. Returns `rooms[{calendarId,name,capacity,rules{maxMinutes,bookAhead,approval},equipment}]` and `statuses[{roomCalendarId,free,reasons}]`. Reason 8 means booked, 1 means longer than the room allows, 4 means taken offline |
| `GET …/BUILDING/day?start=&end=&floor=` | One floor's bookings, at most 2 days |

- Rooms with `rules.approval` need admin approval.
- After booking, read the event detail. The room guest (`category:3`) has `status` 2 when accepted, 1 when pending or awaiting approval.
- Releasing a room with `removeAttendeeCalendarIds` notifies no one.

## 7. Calendars

| Job | Call | Notes |
| --- | --- | --- |
| List | `GET /calendar/calendars` | Returns `{id,isPrimary,canWrite,label,timezone,calendarType}` |
| Show or hide in this client | `PUT /calendar/CALENDAR_ID/visibility {visible}` | Controls only what `/calendar/week` returns. This is **not** sharing |
| Create | `POST /calendar/calendars {summary,description?,timezone?,backgroundColor?}` |  |
| ⚠ Delete a whole calendar | `DELETE /calendar/calendars/ID` | Cannot be undone. Deleting the primary calendar fails with 500 |
| Manage | `GET /calendar/manage/calendars`, `PATCH /calendar/manage/calendars/ID {summary?,note?,backgroundColor?}` |  |
| ⚠ Sharing | `GET`/`PATCH /calendar/manage/calendars/ID/sharing {visibility?:"private"\|"freeBusy"\|"public", add?[{id,role 1–4}], remove?[{id,kind}], update?}` | Grants other people access |
| Subscribe | `GET /calendar/manage/directory?q=`, `POST`/`DELETE /calendar/manage/calendars/ID/subscription` |  |
| Settings | `GET /calendar/settings`, `GET`/`PATCH /calendar/settings/preferences` | Account preferences |
| CalDAV passwords | `/calendar/caldav/passwords` | Credentials. Host use only |
| Place suggestions | `GET /calendar/place-tips?q=` |  |

## 8. Event cards in chats

These act on calendar card messages in the local store:

| Job | Call | Notes |
| --- | --- | --- |
| Card state | `GET /calendar/card-event?calendarId=&key=&originalTime=` | Returns `{available,joined,canJoin}` |
| ⚠ Join from a shared-event card | `POST /calendar/card-event/join {messageId}` | Message type 16. Adds you as a guest |
| ⚠ RSVP from an invite card | `POST /calendar/card-event/rsvp {messageId,reply}` | Message type 18 |
| ⚠ RSVP with a note | `GET`, then `POST /calendar/card-event/rsvp-comment {messageId,reply,comment}` |  |

## 9. Workflows

**Find a slot, book a room, create the event:**

1. Resolve each person's userId (`search.md` / `people.md`).
2. Run `GET /calendar/free-busy?userIds=…&start=…+08:00&end=…`. Optionally narrow it with `POST /calendar/work-hours`.
3. Run `GET /calendar/meeting-rooms/buildings` and match the building by name. Then call `…/availability` once per floor in `floorsWithRooms`, and pick a room with `free:true`. Check `rules.approval` and `maxMinutes`.
4. Run `GET /calendar/calendars` and take the calendar that is `isPrimary` and `canWrite`.
5. Confirm with the user, then `POST /calendar/events {calendarId,title,startAt,endAt,timezone:"Asia/Shanghai",attendeeUserIds,roomCalendarIds}`.
6. Read the week, find the new `sourceId`, then check the room guest's status in the detail.
7. Optionally share the event to a chat.

**Change one occurrence of a recurring event:**

1. From the week, take `sourceId` and that occurrence's `startAt`.
2. Run `PATCH /calendar/event/SOURCE_ID {…, span:"this", instanceStartAt:"<startAt>"}`.
   - To delete it instead, use `DELETE …?span=this&instanceStartAt=…`.

**Answer invites:**

1. In the week view, find events with `selfAttendeeStatus` 1.
2. Answer with `POST /calendar/event/SOURCE_ID/rsvp`.

## Existing meeting settings

`GET /calendar/meetings/:meetingNumber/settings` reads a numeric meeting number accepted by the existing meeting-number validator (9–12 digits, not an event ID). It returns `{canEdit, values, aiSummaryLocked?}`. Absent keys are unsupported, not default values. `canEdit` is true only for an upstream `is_organizer=true`.

⚠ `PATCH` on the same path accepts a nonempty strict object of supported keys: `security` (0 everyone, 1 same tenant, 2 calendar guests), `joinBeforeOwner`, `muteOnJoin`, `lobby`, `autoRecord`, `forbidUnmute`, `hostOnlyShare`, `presenterOnlyAnnotate`, `forbidRename`, `aiSummary`. All except security are booleans. The two `forbid*` flags retain their protocol meaning; true forbids unmuting/changing name. `aiSummary` maps only the minutes-summary feature: native status 1 is disabled/locked, 2 on, 3 off; other/absent statuses are not editable.

The server re-reads immediately before writing, refuses absent fields or a locked feature, and preserves unrelated and unknown protobuf bytes. It reads back after SET and confirms every requested field. Errors are 400 invalid input; 403 `MEETING_SETTINGS_FORBIDDEN`; 409 `MEETING_SETTING_UNSUPPORTED`, `MEETING_SETTINGS_UNAVAILABLE`, or `MEETING_SETTINGS_NOT_APPLIED`. Gateway/readback failures may occur after an applied write: refresh and inspect the result before retrying. No automatic write retry. _(unverified against live gateway)_

These are an existing meeting's settings, distinct from account preferences. They do not create a video meeting, apply pre-settings, bind vc_setting_id to a new event, start recording, join a meeting, or modify the event draft. Native chat/emoji/effects and interpreter editing remain outside this supported subset.
