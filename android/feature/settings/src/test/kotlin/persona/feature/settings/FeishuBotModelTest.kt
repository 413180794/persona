package persona.feature.settings

import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.FeishuBotOwner
import persona.core.contracts.api.FeishuBotSettings
import persona.core.contracts.api.FeishuBotStatus
import persona.core.contracts.api.FeishuBotUpdate
import persona.core.designsystem.UiText
import persona.core.hub.HubError
import persona.core.hub.HubFailure

private class FakeBot(var status: FeishuBotStatus) : FeishuBotSettings {
    val updates = mutableListOf<FeishuBotUpdate>()
    var refuse: String? = null
    override suspend fun status() = status
    override suspend fun update(update: FeishuBotUpdate): FeishuBotStatus {
        updates += update
        refuse?.let { throw HubFailure(HubError.Server, it, status = 400) }
        status = status.copy(enabled = update.enabled ?: status.enabled, appId = update.appId ?: status.appId,
            domain = update.domain ?: status.domain, hasSecret = status.hasSecret || !update.appSecret.isNullOrEmpty(), fromConfig = false)
        return status
    }
}

/** Settings › Lark bot (iOS `FeishuBotModelTests`): loading, the switch, saving credentials and what the hub says back. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class FeishuBotModelTest {
    @Test fun loadingFillsTheFormFromTheHub() = runBlocking {
        // The hub sends the domain it resolved (feishubot.ResolveDomain), the picker shows Feishu / Lark.
        val model = FeishuBotModel(FakeBot(FeishuBotStatus(enabled = true, appId = "cli_a1", domain = "https://larksuite.com", hasSecret = true)))
        model.load()
        val state = model.state.value
        assertEquals("cli_a1", state.appId)
        assertEquals("lark", state.domain)
        assertEquals("", state.appSecret)
        assertFalse(state.canSave) // nothing changed yet
    }

    @Test fun credentialsAreSavedTogetherAndTheSecretIsNotKept() = runBlocking {
        val bot = FakeBot(FeishuBotStatus())
        val model = FeishuBotModel(bot)
        model.load()
        model.edit(appId = " cli_new ")
        assertFalse(model.state.value.canSave) // a first save needs the secret too
        model.edit(appSecret = "s3cret")
        assertTrue(model.state.value.canSave)
        assertTrue(model.saveCredentials())
        assertEquals(FeishuBotUpdate(appId = "cli_new", appSecret = "s3cret", domain = "feishu"), bot.updates.single())
        val state = model.state.value
        assertEquals("", state.appSecret)
        assertTrue(state.status!!.hasSecret)
        assertFalse(state.canSave)
        // Saved once: the app id alone can change, the secret stays as it is on the hub.
        model.edit(domain = "lark")
        assertTrue(model.state.value.canSave)
    }

    @Test fun theHubsReasonIsShownWhenItCantVerify() = runBlocking {
        val bot = FakeBot(FeishuBotStatus(hasSecret = true, appId = "cli_a1")).apply { refuse = "飞书说 App Secret 不对" }
        val model = FeishuBotModel(bot)
        model.load()
        model.edit(appSecret = "wrong")
        assertFalse(model.saveCredentials())
        assertEquals(listOf<Any>(UiText.Plain("飞书说 App Secret 不对")), model.state.value.error!!.args)
        assertEquals("wrong", model.state.value.appSecret) // kept to fix
    }

    @Test fun theSwitchSendsOnlyItself() = runBlocking {
        val bot = FakeBot(FeishuBotStatus(hasSecret = true, appId = "cli_a1"))
        val model = FeishuBotModel(bot)
        model.load()
        model.edit(appId = "cli_editing")
        assertTrue(model.setEnabled(true))
        assertEquals(FeishuBotUpdate(enabled = true), bot.updates.single())
        assertTrue(model.state.value.status!!.enabled)
        assertEquals("cli_editing", model.state.value.appId) // what is being typed stays
    }

    @Test fun theResolvedDomainMapsBackToThePicker() {
        assertEquals("feishu", FeishuBotState.domainChoice("https://open.feishu.cn"))
        assertEquals("feishu", FeishuBotState.domainChoice(""))
        assertEquals("lark", FeishuBotState.domainChoice("https://larksuite.com"))
        assertEquals("https://open.example.com", FeishuBotState.domainChoice("https://open.example.com"))
        val same = FeishuBotState(status = FeishuBotStatus(appId = "cli_a1", domain = "https://open.feishu.cn", hasSecret = true),
            appId = "cli_a1", domain = "feishu")
        assertFalse(same.canSave) // Feishu is what the hub already uses
    }

    @Test fun credentialsFromHubJsonNeedTheSecretTyped() {
        // hub.json's secret isn't the hub's to reuse for a saved copy: updateBot answers needs_secret.
        val fromConfig = FeishuBotState(status = FeishuBotStatus(appId = "cli_a1", domain = "https://open.feishu.cn", hasSecret = true, fromConfig = true),
            appId = "cli_a1", domain = "lark")
        assertFalse(fromConfig.canSave)
        assertTrue(fromConfig.copy(appSecret = "s3cret").canSave)
    }

    @Test fun anotherAppIdNeedsItsSecretAndSaysThePairingGoes() {
        val paired = FeishuBotState(status = FeishuBotStatus(appId = "cli_a1", hasSecret = true, owner = FeishuBotOwner("示例用户", 1)),
            appId = "cli_other")
        assertFalse(paired.canSave) // the old secret won't verify another app
        assertTrue(paired.unpairs)
        assertTrue(paired.copy(appSecret = "s3cret").canSave)
        assertFalse(paired.copy(appId = "cli_a1", domain = "lark").unpairs)
    }

    @Test fun aRefreshTakesTheHubsCredentialsUnlessBeingTyped() = runBlocking {
        val bot = FakeBot(FeishuBotStatus(hasSecret = true, appId = "cli_old"))
        val model = FeishuBotModel(bot)
        model.load()
        bot.status = bot.status.copy(appId = "cli_new") // changed on the web
        model.load()
        assertEquals("cli_new", model.state.value.appId)
        model.edit(appId = "cli_typing")
        bot.status = bot.status.copy(appId = "cli_newer")
        model.load()
        assertEquals("cli_typing", model.state.value.appId)
    }
}
