# Message list density

iPhone 18 Pro / iOS 27.0, synthetic mock chats, standard text size. AXe reads the actual row frames; after the change, all three ordinary chats measure 64 pt in both the new and old layouts, down from 92 pt.

Only chat rows on the "Messages" page changed: avatars use the existing 48 pt token, the List's top and bottom row insets are 0, and content retains 8 pt of padding above and below. Dynamic Type does not use a fixed row height: at the system accessibility-large size, the actual height is 95.67 pt. Titles and previews keep their existing single-line truncation. The Inbox view and input field are unchanged.

| Scenario | Screenshot |
| --- | --- |
| Before, 92 pt | [Before](before.png) |
| New layout, light glass, 64 pt | [New layout, light](new-light.png) |
| New layout, dark BeautifulUI, 64 pt | [New layout, dark](new-dark.png) |
| Old layout, light glass, 64 pt | [Old layout, light](old-light.png) |
| Old layout, dark BeautifulUI, 64 pt | [Old layout, dark](old-dark.png) |
| Accessibility text size, 95.67 pt | [Accessibility text size](large-type.png) |
