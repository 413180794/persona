package jarvis.android.mail

import jarvis.android.data.ApiClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.concurrent.ConcurrentHashMap

/**
 * What the mail pages share in the process: the hub's accounts (no secrets: the API never returns them) for 写邮件's
 * 发件人, 回复全部 and 设置 › 连接器; Feishu's status for its row there; mails already read, so opening one again is
 * instant. Every page that shows them reloads; screenshot tests fill them ahead (a snapshot is one frame).
 */
object MailStore {
    private val _accounts = MutableStateFlow<List<MailAccount>?>(null)
    /** Null until the first answer from the hub. */
    val accounts: StateFlow<List<MailAccount>?> = _accounts.asStateFlow()

    private val _lark = MutableStateFlow<LarkStatus?>(null)
    val lark: StateFlow<LarkStatus?> = _lark.asStateFlow()

    private val messages = ConcurrentHashMap<String, MailMessage>()

    fun account(id: String?): MailAccount? = _accounts.value?.firstOrNull { it.id == id }

    /** Pulls the list; failures keep what is there (thrown to the caller who wants them). */
    suspend fun reloadAccounts(api: ApiClient): List<MailAccount> = api.mailAccounts().also { _accounts.value = it }

    suspend fun reloadLark(api: ApiClient): LarkStatus = api.larkStatus().also { _lark.value = it }

    fun message(accountId: String, mailbox: String, uid: Long): MailMessage? = messages[key(accountId, mailbox, uid)]

    suspend fun loadMessage(api: ApiClient, accountId: String, mailbox: String, uid: Long): MailMessage =
        message(accountId, mailbox, uid) ?: api.mailMessage(accountId, uid, mailbox).also { put(accountId, mailbox, it) }

    fun put(accountId: String, mailbox: String, m: MailMessage) { messages[key(accountId, mailbox, m.uid)] = m }

    fun setAccounts(list: List<MailAccount>) { _accounts.value = list }
    fun setLark(st: LarkStatus) { _lark.value = st }

    private fun key(accountId: String, mailbox: String, uid: Long) = "$accountId/$mailbox/$uid"
}
