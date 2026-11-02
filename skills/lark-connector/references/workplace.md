# Workplace: apps, help desks, canteen, red packets, Lingo

Paths are relative to `/agent/v1`.

- ⚠ = a write that others see, that spends money, or that cannot be undone. Confirm it first.
- _(unverified)_ = never run against the real Lark gateway. Read it back afterwards.

## Workplace apps

| Call | Notes |
| --- | --- |
| `GET /workplace/apps?locale=zh-CN\|en-US` | Home apps: `groups[].apps[{itemId,appId?,name,description,url?,larkUrl?}]`, `commonItemIds[]`. Any other `locale` returns 400 |
| `GET /workplace/catalog/tags` | Categories. An `id` can be several ids joined with commas (`"10,65"`); pass it as-is |
| `GET /workplace/catalog/items?tags=<1–5 ids>` | Apps in those categories. Up to 100 per category, no paging (`truncated`) |
| `GET /workplace/approval-badge` | `{count}` of pending approvals (details are in `approvals.md`) |
| `POST /workplace/common {itemIds:[1–100]}`, `DELETE /workplace/common/ITEM`, `PUT /workplace/common/order {itemIds}` | Your personal favourite apps. The `order` list must contain exactly the current set, or 409 `COMMON_APPS_CHANGED` |

## Help desks

1. `GET /workplace/helpdesks` returns `desks[{id,name,description,feedId?}]`. Match the desk by name.
2. If the desk has no `feedId`, call `POST /workplace/helpdesks/ID/chat`. It returns `{feedId}`, **only opens the chat and sends nothing**, and may create a new ticket chat _(unverified)_.
3. Send the question with `POST /conversations/FEED_ID/messages` (`messaging.md`).

## Canteen (real orders)

Every write here acts on the user's real meal account. Confirm the building, date, meal and dish first.

| Step | Call | Notes |
| --- | --- | --- |
| ⚠ Connect | `POST /workplace/canteen/connect` (optional `{"locale":"zh-CN"}`) | **Uses the current Lark session to approve the canteen app's OAuth on the user's behalf.** The cookie is stored locally for up to 7 days. Needed when other canteen reads return 401 `CANTEEN_AUTH_REQUIRED` |
| Overview | `GET /workplace/canteen/overview` | Returns `{buildings[{mdmCode (= buildingCode),regionName}]}` |
| Bookable dates | `GET /workplace/canteen/calendar?buildingCode=MDBD…` | `dates[{date,canReserve}]` |
| Menu | `GET /workplace/canteen/menu?buildingCode=&mealDate=YYYY-MM-DD&timeCode=breakfast\|lunch\|dinner` | Returns `{hadOrdered, orderLimitedTime, menuSites[{items[{id,name,currentStock,pickupAddress}]}]}` |
| Buffet, rules | `GET /workplace/canteen/buffet`, `…/buffet-calendar`, `…/rules` |  |
| ⚠ **Order** | `POST /workplace/canteen/orders {buildingCode,mealDate,timeCode,itemId,requestId:<UUID>}` | `itemId` comes from the menu's `items[].id`. **HTTP 200 can still be a failure**: check that `successOrders[0].orderId` exists and that `failOrders` is empty. 409 `CANTEEN_ORDER_UNAVAILABLE` or `CANTEEN_ORDER_PENDING`. `requestId` dedupe lives in process memory (last 100), so a restart forgets it |
| My orders | `POST /workplace/canteen/orders/list {status:0 all\|1 booked\|2 cancelled\|3 done\|4 expired\|16 released, start, end, cursor?}` | `start` and `end` are `YYYY-MM-DD` in +08:00. Page with `cursor` = `lastRecordInfos` |
| ⚠ Cancel | `POST /workplace/canteen/orders/cancel {orderId}` | Only before `cancelLimitedTime` (+08:00). The meal may not be orderable again |

Before ordering, confirm on the menu that `hadOrdered:false` and `currentStock>0`. After ordering, read the order back with `orders/list` (`status:1`).

## Red packets (read-only)

| Call | Notes |
| --- | --- |
| `GET /hongbao/records?kind=received\|sent&year=2026&cursor=` | **`kind` is required**; without it you get 400 `INVALID_HONGBAO_INPUT`. 20 per page. Returns `records[{id,amountFen,at,chatId (= feedId),chatName,counterpart,isP2p}]`, `totalAmountFen`, `totalCount`, `nextCursor` |
| `GET /hongbao/ID` | `{subject,sender,status,totalAmountFen,totalCount,grabbedCount,isRandom,hasGrabbed,myAmountFen?,receivers[≤200]}` |

Amounts are in **fen**; divide by 100 for yuan. The API cannot send, grab or open red packets, and it does not expose the wallet balance.

## Lingo (enterprise glossary)

| Call | Notes |
| --- | --- |
| `GET /lingo/lookup --url-query 'query=OKR'` | Returns `{entry?, meanings[{id,name,description}]}`. A query longer than 40 characters after collapsing whitespace returns empty |
| `GET /lingo/card?id=&query=` | The full entry: `fullName`, `description`, `aliases`, `links`, `relatedWords`. Ids come from `lookup`, or from a message part's `metadata.lingo[].id` |
| `GET /lingo/editor` | Editor link |
| ⚠ `POST /lingo/feedback {id,type:"wrongMeaning"\|"contentMistake"\|"lackContent"\|"other",content,query?}` | Sent to the glossary team. _(unverified)_ |
