package jarvis.android.ui.design

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBackIos
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.rounded.LibraryBooks
import androidx.compose.material.icons.automirrored.rounded.Reply
import androidx.compose.material.icons.automirrored.rounded.Send
import androidx.compose.material.icons.automirrored.rounded.ShortText
import androidx.compose.material.icons.automirrored.rounded.ViewList
import androidx.compose.material.icons.rounded.Add
import androidx.compose.material.icons.rounded.Alarm
import androidx.compose.material.icons.rounded.AlternateEmail
import androidx.compose.material.icons.rounded.ArrowUpward
import androidx.compose.material.icons.rounded.AutoAwesome
import androidx.compose.material.icons.rounded.Cancel
import androidx.compose.material.icons.rounded.ChatBubbleOutline
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.Computer
import androidx.compose.material.icons.rounded.ContentCopy
import androidx.compose.material.icons.rounded.DarkMode
import androidx.compose.material.icons.rounded.Delete
import androidx.compose.material.icons.rounded.Devices
import androidx.compose.material.icons.rounded.Dns
import androidx.compose.material.icons.rounded.DragHandle
import androidx.compose.material.icons.rounded.Edit
import androidx.compose.material.icons.rounded.EditNote
import androidx.compose.material.icons.rounded.Email
import androidx.compose.material.icons.rounded.Error
import androidx.compose.material.icons.rounded.ErrorOutline
import androidx.compose.material.icons.rounded.ExpandLess
import androidx.compose.material.icons.rounded.ExpandMore
import androidx.compose.material.icons.rounded.FilterList
import androidx.compose.material.icons.rounded.Folder
import androidx.compose.material.icons.rounded.Forum
import androidx.compose.material.icons.rounded.Groups
import androidx.compose.material.icons.rounded.HelpOutline
import androidx.compose.material.icons.rounded.History
import androidx.compose.material.icons.rounded.Inbox
import androidx.compose.material.icons.rounded.Info
import androidx.compose.material.icons.rounded.Key
import androidx.compose.material.icons.rounded.Link
import androidx.compose.material.icons.rounded.Memory
import androidx.compose.material.icons.rounded.Mic
import androidx.compose.material.icons.rounded.MoreHoriz
import androidx.compose.material.icons.rounded.Notifications
import androidx.compose.material.icons.rounded.NotificationsOff
import androidx.compose.material.icons.rounded.Palette
import androidx.compose.material.icons.rounded.PanTool
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.Photo
import androidx.compose.material.icons.rounded.PhotoCamera
import androidx.compose.material.icons.rounded.PhoneAndroid
import androidx.compose.material.icons.rounded.PlayArrow
import androidx.compose.material.icons.rounded.Psychology
import androidx.compose.material.icons.rounded.PushPin
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material.icons.rounded.Repeat
import androidx.compose.material.icons.rounded.Schedule
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.Settings
import androidx.compose.material.icons.rounded.Share
import androidx.compose.material.icons.rounded.Shield
import androidx.compose.material.icons.rounded.Speed
import androidx.compose.material.icons.rounded.Stop
import androidx.compose.material.icons.rounded.Storage
import androidx.compose.material.icons.rounded.Terminal
import androidx.compose.material.icons.rounded.Tune
import androidx.compose.material.icons.rounded.UnfoldLess
import androidx.compose.material.icons.rounded.UnfoldMore
import androidx.compose.material.icons.rounded.ViewAgenda
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.material.icons.rounded.WifiOff
import androidx.compose.material.icons.rounded.Layers
import androidx.compose.material.icons.rounded.Assignment
import androidx.compose.material.icons.rounded.Attachment
import androidx.compose.material.icons.rounded.Description
import androidx.compose.material.icons.rounded.Badge
import androidx.compose.material.icons.rounded.BatteryChargingFull
import androidx.compose.material.icons.rounded.RestartAlt
import androidx.compose.material.icons.rounded.OpenInFull
import androidx.compose.material.icons.rounded.FormatQuote
import androidx.compose.material.icons.rounded.TextFields
import androidx.compose.material.icons.automirrored.rounded.OpenInNew
import androidx.compose.material.icons.rounded.RadioButtonUnchecked
import androidx.compose.material.icons.rounded.Circle
import androidx.compose.material.icons.rounded.TripOrigin
import androidx.compose.material.icons.rounded.BrokenImage
import androidx.compose.material.icons.automirrored.rounded.Subject
import androidx.compose.material.icons.rounded.SwapHoriz
import androidx.compose.material.icons.automirrored.rounded.ShowChart
import androidx.compose.material.icons.rounded.Watch
import androidx.compose.material.icons.rounded.Public
import androidx.compose.material.icons.rounded.Language
import androidx.compose.material.icons.rounded.Inventory
import androidx.compose.material.icons.automirrored.rounded.ListAlt
import androidx.compose.material.icons.rounded.SmsFailed
import androidx.compose.material.icons.rounded.Sell
import androidx.compose.material.icons.rounded.People
import androidx.compose.material.icons.rounded.Videocam
import androidx.compose.material.icons.rounded.CalendarMonth
import androidx.compose.material.icons.rounded.SubdirectoryArrowRight
import androidx.compose.material.icons.rounded.SkipNext
import androidx.compose.material.icons.rounded.ContactPage
import androidx.compose.material.icons.rounded.WbTwilight
import androidx.compose.material.icons.rounded.Bedtime
import androidx.compose.material.icons.rounded.Checklist
import androidx.compose.material.icons.rounded.Radar
import androidx.compose.material.icons.rounded.QrCodeScanner
import androidx.compose.material.icons.rounded.Download
import androidx.compose.material.icons.rounded.Draw
import androidx.compose.material.icons.automirrored.rounded.InsertDriveFile
import androidx.compose.material.icons.rounded.DataObject
import androidx.compose.material.icons.rounded.PictureAsPdf
import androidx.compose.material.icons.rounded.AccountTree
import androidx.compose.material.icons.rounded.ArrowDownward
import androidx.compose.material.icons.rounded.Cloud
import androidx.compose.material.icons.rounded.CloudOff
import androidx.compose.material.icons.rounded.Visibility
import androidx.compose.material.icons.rounded.VisibilityOff
import androidx.compose.material.icons.rounded.Tag
import androidx.compose.material.icons.rounded.MicOff
import androidx.compose.material.icons.rounded.RecordVoiceOver
import androidx.compose.material.icons.rounded.CallEnd
import androidx.compose.ui.graphics.vector.ImageVector

/**
 * The icon vocabulary (iOS `Theme.Icon`, apple/Shared/Design/Icons.swift): one meaning, one Material symbol, in the
 * rounded set (the closest to SF Symbols). Pages write `JIcons.copy`, never an `Icons.…` of their own, so a meaning
 * keeps one look everywhere (IconVocabularyTest).
 */
object JIcons {
    // Status and feedback.
    val error: ImageVector = Icons.Rounded.Warning
    val errorOutline: ImageVector = Icons.Rounded.ErrorOutline
    val critical: ImageVector = Icons.Rounded.Error
    val success: ImageVector = Icons.Rounded.CheckCircle
    val done: ImageVector = Icons.Rounded.Check
    val info: ImageVector = Icons.Rounded.Info
    val offline: ImageVector = Icons.Rounded.WifiOff
    val time: ImageVector = Icons.Rounded.Schedule
    val muted: ImageVector = Icons.Rounded.NotificationsOff
    val notifications: ImageVector = Icons.Rounded.Notifications
    val pinned: ImageVector = Icons.Rounded.PushPin
    val recent: ImageVector = Icons.Rounded.History
    val recurring: ImageVector = Icons.Rounded.Repeat
    val schedule: ImageVector = Icons.Rounded.Alarm
    // Interactions (what an agent waits on).
    val question: ImageVector = Icons.Rounded.HelpOutline
    val plan: ImageVector = Icons.Rounded.Assignment
    val permission: ImageVector = Icons.Rounded.Key
    val askFirst: ImageVector = Icons.Rounded.PanTool
    val allowEverything: ImageVector = Icons.Rounded.Shield
    val effort: ImageVector = Icons.Rounded.Speed
    // Actions.
    val copy: ImageVector = Icons.Rounded.ContentCopy
    val close: ImageVector = Icons.Rounded.Close
    val clear: ImageVector = Icons.Rounded.Cancel
    val more: ImageVector = Icons.Rounded.MoreHoriz
    val delete: ImageVector = Icons.Rounded.Delete
    val edit: ImageVector = Icons.Rounded.Edit
    val compose: ImageVector = Icons.Rounded.EditNote
    val drawer: ImageVector = Icons.Rounded.DragHandle
    val dictate: ImageVector = Icons.Rounded.Mic
    val share: ImageVector = Icons.Rounded.Share
    val retry: ImageVector = Icons.Rounded.Refresh
    val interrupt: ImageVector = Icons.Rounded.Stop
    val run: ImageVector = Icons.Rounded.PlayArrow
    val add: ImageVector = Icons.Rounded.Add
    val send: ImageVector = Icons.Rounded.ArrowUpward
    val sendMail: ImageVector = Icons.AutoMirrored.Rounded.Send
    val reply: ImageVector = Icons.AutoMirrored.Rounded.Reply
    val options: ImageVector = Icons.Rounded.Tune
    val filter: ImageVector = Icons.Rounded.FilterList
    val settings: ImageVector = Icons.Rounded.Settings
    val link: ImageVector = Icons.Rounded.Link
    val pin: ImageVector = Icons.Rounded.PushPin
    val mute: ImageVector = Icons.Rounded.NotificationsOff
    val unmute: ImageVector = Icons.Rounded.Notifications
    val collapseAll: ImageVector = Icons.Rounded.UnfoldLess
    val expandAll: ImageVector = Icons.Rounded.UnfoldMore
    val attach: ImageVector = Icons.Rounded.Attachment
    val style: ImageVector = Icons.Rounded.Palette
    val appearance: ImageVector = Icons.Rounded.DarkMode
    // Navigation.
    val disclosure: ImageVector = Icons.AutoMirrored.Rounded.KeyboardArrowRight
    val back: ImageVector = Icons.AutoMirrored.Rounded.ArrowBackIos
    val expand: ImageVector = Icons.Rounded.ExpandMore
    val collapse: ImageVector = Icons.Rounded.ExpandLess
    // Things.
    val ai: ImageVector = Icons.Rounded.AutoAwesome
    val agents: ImageVector = Icons.Rounded.Code
    val terminal: ImageVector = Icons.Rounded.Terminal
    val model: ImageVector = Icons.Rounded.Layers
    val space: ImageVector = Icons.Rounded.Layers
    val conversation: ImageVector = Icons.Rounded.ChatBubbleOutline
    val document: ImageVector = Icons.Rounded.Description
    val folder: ImageVector = Icons.Rounded.Folder
    val photo: ImageVector = Icons.Rounded.Photo
    val camera: ImageVector = Icons.Rounded.PhotoCamera
    val text: ImageVector = Icons.AutoMirrored.Rounded.ShortText
    val machines: ImageVector = Icons.Rounded.Devices
    val machine: ImageVector = Icons.Rounded.Computer
    val server: ImageVector = Icons.Rounded.Dns
    val phone: ImageVector = Icons.Rounded.PhoneAndroid
    val person: ImageVector = Icons.Rounded.Person
    val inbox: ImageVector = Icons.Rounded.Inbox
    val mail: ImageVector = Icons.Rounded.Email
    val feishuChats: ImageVector = Icons.Rounded.Forum
    val groupChat: ImageVector = Icons.Rounded.Groups
    val mention: ImageVector = Icons.Rounded.AlternateEmail
    val credentials: ImageVector = Icons.Rounded.Badge
    val memory: ImageVector = Icons.Rounded.Psychology
    val thinking: ImageVector = Icons.Rounded.Psychology
    val search: ImageVector = Icons.Rounded.Search
    val knowledge: ImageVector = Icons.AutoMirrored.Rounded.LibraryBooks
    val storage: ImageVector = Icons.Rounded.Storage
    val cpu: ImageVector = Icons.Rounded.Memory
    val cards: ImageVector = Icons.Rounded.ViewAgenda
    val compact: ImageVector = Icons.AutoMirrored.Rounded.ViewList
    val key: ImageVector = Icons.Rounded.Key
    val battery: ImageVector = Icons.Rounded.BatteryChargingFull
    val autostart: ImageVector = Icons.Rounded.RestartAlt
    val enlarge: ImageVector = Icons.Rounded.OpenInFull
    val chatInstead: ImageVector = Icons.Rounded.ChatBubbleOutline
    val quote: ImageVector = Icons.Rounded.FormatQuote
    val select: ImageVector = Icons.Rounded.TextFields
    val handToAI: ImageVector = Icons.AutoMirrored.Rounded.Send
    val addContext: ImageVector = Icons.Rounded.AlternateEmail
    val openExternal: ImageVector = Icons.AutoMirrored.Rounded.OpenInNew
    val unchecked: ImageVector = Icons.Rounded.RadioButtonUnchecked
    val dot: ImageVector = Icons.Rounded.Circle
    val inProgress: ImageVector = Icons.Rounded.TripOrigin
    val brokenImage: ImageVector = Icons.Rounded.BrokenImage
    val output: ImageVector = Icons.AutoMirrored.Rounded.Subject
    val fromPeer: ImageVector = Icons.Rounded.SwapHoriz
    val chart: ImageVector = Icons.AutoMirrored.Rounded.ShowChart
    val watch: ImageVector = Icons.Rounded.Watch
    val web: ImageVector = Icons.Rounded.Public
    val translate: ImageVector = Icons.Rounded.Language
    val box: ImageVector = Icons.Rounded.Inventory
    /** An index or a table of contents. */
    val list: ImageVector = Icons.AutoMirrored.Rounded.ListAlt
    val failedMessage: ImageVector = Icons.Rounded.SmsFailed
    /** 打标: an entry's importance, relevance and tags by hand. */
    val tag: ImageVector = Icons.Rounded.Sell
    /** A project in the knowledge base. */
    val project: ImageVector = Icons.Rounded.Layers
    /** People. */
    val people: ImageVector = Icons.Rounded.People
    /** A meeting. */
    val meeting: ImageVector = Icons.Rounded.Videocam
    /** A calendar event. */
    val event: ImageVector = Icons.Rounded.CalendarMonth
    /** A one-off follow-up: Jarvis comes back to something later. */
    val followOn: ImageVector = Icons.Rounded.SubdirectoryArrowRight
    /** A run that was skipped. */
    val skipped: ImageVector = Icons.Rounded.SkipNext
    /** 关于我: what the user told Jarvis. */
    val aboutMe: ImageVector = Icons.Rounded.ContactPage
    /** A morning brief. */
    val morning: ImageVector = Icons.Rounded.WbTwilight
    /** An evening review. */
    val evening: ImageVector = Icons.Rounded.Bedtime
    /** A to-do round-up. */
    val todo: ImageVector = Icons.Rounded.Checklist
    /** A patrol over the agents. */
    val patrol: ImageVector = Icons.Rounded.Radar
    /** Scan a pairing QR code. */
    val qrScan: ImageVector = Icons.Rounded.QrCodeScanner
    /** Save to the phone, the gallery. */
    val save: ImageVector = Icons.Rounded.Download
    /** Mark up a picture. */
    val markup: ImageVector = Icons.Rounded.Draw
    /** A file of no particular kind. */
    val file: ImageVector = Icons.AutoMirrored.Rounded.InsertDriveFile
    /** A code or config file: curly braces (`</>` is only Agents). */
    val code: ImageVector = Icons.Rounded.DataObject
    val pdf: ImageVector = Icons.Rounded.PictureAsPdf
    /** A Git branch, a repository. */
    val branch: ImageVector = Icons.Rounded.AccountTree
    /** Commits ahead of / behind the upstream. */
    val ahead: ImageVector = Icons.Rounded.ArrowUpward
    val behind: ImageVector = Icons.Rounded.ArrowDownward
    /** The upstream branch, or none. */
    val upstream: ImageVector = Icons.Rounded.Cloud
    val noUpstream: ImageVector = Icons.Rounded.CloudOff
    /** Show / hide what is hidden. */
    val view: ImageVector = Icons.Rounded.Visibility
    val hide: ImageVector = Icons.Rounded.VisibilityOff
    /** An identifier to copy: a commit's hash, a session id. */
    val id: ImageVector = Icons.Rounded.Tag
    /** No microphone; muted. */
    val micOff: ImageVector = Icons.Rounded.MicOff
    /** A voice (音色). */
    val voice: ImageVector = Icons.Rounded.RecordVoiceOver
    /** End a voice conversation. */
    val hangUp: ImageVector = Icons.Rounded.CallEnd
}
