package persona.feature.settings

import persona.core.contracts.api.*

/** A connectors API whose every call fails unless a test overrides it. */
internal abstract class ConnectorsApiStub : ConnectorsManagement {
    private fun unused(): Nothing = throw AssertionError("not used in this test")
    override suspend fun connectors(all: Boolean): List<HubConnector> = unused()
    override suspend fun setConnectorEnabled(id: String, enabled: Boolean): HubConnector = unused()
    override suspend fun setConnectorFeed(id: String, feed: String, on: Boolean): HubConnector = unused()
    override suspend fun deleteConnectorData(id: String): ConnectorDataDeleted = unused()
    override suspend fun deleteConnectorArchive(id: String): ConnectorDataDeleted = unused()
    override suspend fun mailAccounts(): List<MailAccount> = unused()
    override suspend fun addMailAccount(req: AddMailAccountRequest): MailAccount = unused()
    override suspend fun deleteMailAccount(id: String): Unit = unused()
    override suspend fun testMailAccount(id: String): MailTestResult = unused()
    override suspend fun googleSignInStart(loginHint: String?): GoogleStart = unused()
    override suspend fun googleSignInFinish(finish: GoogleFinish): MailAccount = unused()
    override suspend fun larkStatus(): LarkStatus = unused()
    override suspend fun larkAccounts(): LarkAccounts? = unused()
    override suspend fun larkChatChoices(): List<LarkChatChoice> = unused()
}
