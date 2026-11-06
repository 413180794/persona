package persona.feature.agents

import persona.core.designsystem.matches
import androidx.lifecycle.viewModelScope
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.api.*
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import java.io.IOException

/** "New Agent"'s model picker: the list of the launcher that will run, the pick sent once, efforts it takes. */
/** Robolectric for the strings: Simplified Chinese, the language the expected text is in. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentModelTest {
    private val res get() = RuntimeEnvironment.getApplication().resources

    private val machines = MutableStateFlow(listOf(Machine("first", online = true), Machine("second", online = true)))
    private val all = listOf("low", "medium", "high", "xhigh", "max")
    private val claude = LauncherModels("claude", listOf(
        AgentModel("default", "Default (recommended)", resolved = "claude-opus-5-5", isDefault = true, efforts = all),
        AgentModel("opus", "Opus 5.5", resolved = "claude-opus-5-5", efforts = all),
        AgentModel("sonnet", "Sonnet 5.5", resolved = "claude-sonnet-5-5", efforts = listOf("low", "medium", "high", "max")),
        AgentModel("haiku", "Haiku 5.5", resolved = "claude-haiku-5-5"),
        AgentModel("gateway/glm-5.3", "glm-5.3", description = "From gateway", efforts = listOf("low", "medium", "high")),
    ))
    private val codex = LauncherModels("codex", listOf(
        AgentModel("gpt-5.5", "GPT-5.5", isDefault = true, efforts = listOf("low", "medium", "high"), defaultEffort = "medium"),
        AgentModel("gpt-5.5-mini", "GPT-5.5-Mini", efforts = listOf("low", "medium")),
    ))

    private fun test(block: suspend TestScope.() -> Unit) = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        try { block() } finally { Dispatchers.resetMain() }
    }

    private fun service() = Service().apply {
        available = listOf(AgentLauncher("claude"), AgentLauncher("sonnetcc"), AgentLauncher("codex", agentType = "codex"), AgentLauncher("traex", agentType = "traex"))
        lists = mapOf("claude" to claude, "sonnetcc" to claude.copy(locked = "启动器固定了模型参数"), "codex" to codex,
            "traex" to LauncherModels("traex"))
    }

    @Test fun theDefaultIsPickedAndNothingIsSentUntilAnotherModelIs() = test {
        val service = service()
        val store = MemoryStore()
        val model = NewAgentViewModel(service, store, machines)
        try {
            advanceUntilIdle()
            val state = model.state.value
            assertEquals("claude", state.selection.launcher)
            assertEquals(listOf(Call("first", "claude", false)), service.calls)
            assertEquals(claude, state.modelList)
            assertTrue(state.canPickModel)
            assertEquals("", state.model)
            assertNull(state.sentModel)
            assertEquals("claude-opus-5-5", defaultModelDetail(res, state))
            model.pickModel("gateway/glm-5.3")
            assertEquals("glm-5.3", model.state.value.pickedModel?.label)
            assertEquals("gateway/glm-5.3", model.state.value.sentModel)
            // The default entry is no pick, like none.
            model.pickModel("default")
            assertEquals("", model.state.value.model)
            assertNull(model.state.value.sentModel)
            model.submit()
            advanceUntilIdle()
            assertNull(service.request?.model)
        } finally { model.viewModelScope.cancel() }
        store.operation = null
        val picked = NewAgentViewModel(service, store, machines)
        try {
            advanceUntilIdle()
            picked.pickModel("opus")
            picked.submit()
            advanceUntilIdle()
            assertEquals("opus", service.request?.model)
        } finally { picked.viewModelScope.cancel() }
        // Never remembered: the next form starts from the default again.
        store.operation = null
        val next = NewAgentViewModel(service, store, machines)
        try {
            advanceUntilIdle()
            assertEquals("claude", next.state.value.selection.launcher)
            assertEquals("", next.state.value.model)
        } finally { next.viewModelScope.cancel() }
    }

    @Test fun anotherLauncherOrMachineGoesBackToTheDefaultAndReadsItsList() = test {
        val service = service()
        val store = MemoryStore()
        val model = NewAgentViewModel(service, store, machines)
        try {
            advanceUntilIdle()
            model.pickModel("opus")
            model.selection(model.state.value.selection.copy(launcher = "codex"))
            assertEquals("", model.state.value.model)
            // Loading: the old launcher's list is not shown for the new one.
            assertNull(model.state.value.modelList)
            assertTrue(model.state.value.models.loading)
            advanceUntilIdle()
            assertEquals(codex, model.state.value.modelList)
            model.pickModel("gpt-5.5-mini")
            // A mode or effort change keeps the pick and does not read the list again.
            model.selection(model.state.value.selection.copy(mode = "plan"))
            assertEquals("gpt-5.5-mini", model.state.value.model)
            model.selectMachine("second")
            assertEquals("", model.state.value.model)
            advanceUntilIdle()
            assertEquals(listOf(Call("first", "claude", false), Call("first", "codex", false), Call("second", "claude", false)), service.calls)
            assertEquals("claude", model.state.value.modelLauncher)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun effortsFollowTheModelThatWillRun() = test {
        val service = service()
        val model = NewAgentViewModel(service, MemoryStore().apply { saved["first"] = AgentDefaults("/code", "claude", effort = "xhigh") }, machines)
        try {
            advanceUntilIdle()
            // No pick: the default entry's efforts, which take the remembered one.
            assertEquals(all, model.state.value.modelEfforts)
            assertEquals("xhigh", model.state.value.selection.effort)
            assertEquals(listOf("", "low", "medium", "high", "xhigh", "max"), effortChoices(model.state.value.modelEfforts).map { it.first })
            // Sonnet does not take xhigh: back to "Follow launcher", and its levels only.
            model.pickModel("sonnet")
            assertEquals("", model.state.value.selection.effort)
            assertEquals(listOf("", "low", "medium", "high", "max"), effortChoices(model.state.value.modelEfforts).map { it.first })
            model.selection(model.state.value.selection.copy(effort = "max"))
            model.pickModel("opus")
            assertEquals("max", model.state.value.selection.effort)
            // Haiku has no effort setting.
            model.pickModel("haiku")
            assertEquals(emptyList<String>(), model.state.value.modelEfforts)
            assertEquals("", model.state.value.selection.effort)
            model.submit()
            advanceUntilIdle()
            assertEquals("haiku", service.request?.model)
            assertNull(service.request?.effort)
        } finally { model.viewModelScope.cancel() }
        // Without a list every level stays offered.
        assertEquals(effortLevels, effortChoices(null))
    }

    @Test fun codexSendsTheModelAndNeverAnEffort() = test {
        val service = service()
        val model = NewAgentViewModel(service, MemoryStore().apply { saved["first"] = AgentDefaults("/code", "codex", effort = "high") }, machines)
        try {
            advanceUntilIdle()
            assertEquals("codex", model.state.value.agentType)
            assertEquals("5.5", defaultModelDetail(res, model.state.value))
            model.pickModel("gpt-5.5-mini")
            model.prompt("修一下登录")
            model.submit()
            advanceUntilIdle()
            assertEquals("gpt-5.5-mini", service.request?.model)
            assertNull(service.request?.effort)
            assertEquals("codex", service.request?.agentType)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun aLockedLauncherOrOneWithoutAListTakesNoPick() = test {
        val service = service()
        val model = NewAgentViewModel(service, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(launcher = "sonnetcc"))
            advanceUntilIdle()
            val locked = model.state.value
            assertEquals("启动器固定了模型参数", locked.modelList?.locked)
            assertFalse(locked.canPickModel)
            model.pickModel("opus")
            assertEquals("", model.state.value.model)
            model.submit()
            advanceUntilIdle()
            assertNull(service.request?.model)
        } finally { model.viewModelScope.cancel() }
        val traex = NewAgentViewModel(service, MemoryStore().apply { saved["first"] = AgentDefaults("/code", "traex") }, machines)
        try {
            advanceUntilIdle()
            assertEquals("traex", traex.state.value.models.launcher)
            assertNull(traex.state.value.modelList)
            assertNull(traex.state.value.modelEfforts)
        } finally { traex.viewModelScope.cancel() }
    }

    @Test fun olderHubsAndFailuresNeverHoldUpStarting() = test {
        val old = service().apply { lists = emptyMap() }
        val model = NewAgentViewModel(old, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            // 404: no list, no error, the form as before.
            assertNull(model.state.value.modelList)
            assertNull(model.state.value.models.error)
            assertFalse(model.state.value.models.loading)
            model.submit()
            advanceUntilIdle()
            assertEquals(1, old.starts)
            assertNull(old.request?.model)
        } finally { model.viewModelScope.cancel() }

        val failing = service().apply { failure = IOException("reset") }
        val failed = NewAgentViewModel(failing, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            assertEquals("连不上 hub", failed.state.value.models.error?.resolve(res))
            assertNull(failed.state.value.modelList)
            failed.submit()
            advanceUntilIdle()
            assertEquals(1, failing.starts)
            assertNull(failing.request?.model)
        } finally { failed.viewModelScope.cancel() }

        val retried = NewAgentViewModel(failing, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            assertNotNull(retried.state.value.models.error)
            // Retrying asks the CLI again; the row shows it loading meanwhile.
            val gate = CompletableDeferred<Unit>()
            failing.failure = null
            failing.gate = gate
            retried.reloadModels()
            advanceUntilIdle()
            assertTrue(retried.state.value.models.loading)
            assertNull(retried.state.value.models.error)
            gate.complete(Unit)
            advanceUntilIdle()
            assertEquals(claude, retried.state.value.modelList)
            assertEquals(Call("first", "claude", true), failing.calls.last())
        } finally { retried.viewModelScope.cancel() }
    }

    @Test fun aRefreshKeepsTheListAndDropsAPickItNoLongerHas() = test {
        val service = service()
        val model = NewAgentViewModel(service, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            model.pickModel("gateway/glm-5.3")
            service.lists = service.lists + ("claude" to claude.copy(models = claude.models.dropLast(1)))
            val gate = CompletableDeferred<Unit>()
            service.gate = gate
            model.reloadModels()
            advanceUntilIdle()
            assertTrue(model.state.value.models.loading)
            assertEquals("gateway/glm-5.3", model.state.value.sentModel)
            gate.complete(Unit)
            advanceUntilIdle()
            assertEquals("", model.state.value.model)
            assertEquals(4, model.state.value.modelList?.models?.size)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun followingTheConfigurationListsItsLauncherAndItsModel() = test {
        val service = service().apply {
            model = ModelPurposeDetail(ModelPurpose("agent.new", effective = ModelBinding("launcher.claude", "sonnet")),
                listOf(ModelProvider("launcher.claude", "claude")))
        }
        val model = NewAgentViewModel(service, MemoryStore(), machines)
        try {
            advanceUntilIdle()
            val state = model.state.value
            assertTrue(state.following)
            assertEquals("claude", state.modelLauncher)
            assertEquals(claude, state.modelList)
            // With no pick "Model configuration"'s model runs, so its efforts are the ones offered.
            assertEquals("模型配置：sonnet", defaultModelDetail(res, state))
            assertEquals(listOf("low", "medium", "high", "max"), state.modelEfforts)
            // A launcher picked for this start runs its own default.
            model.selection(state.selection.copy(launcher = "codex"))
            advanceUntilIdle()
            assertEquals("", model.state.value.configuredModel)
            assertEquals(codex, model.state.value.modelList)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun modelsReadAndSearchAsTheListShowsThem() {
        val (default, opus, _, _, glm) = claude.models
        assertEquals("default → claude-opus-5-5", modelDetail(default))
        assertEquals("opus → claude-opus-5-5", modelDetail(opus))
        assertEquals("gateway/glm-5.3", modelDetail(glm))
        assertNull(modelDetail(codex.models[0]))
        assertNull(modelDetail(AgentModel("sonnet", "")))
        assertTrue(glm.matches("GATEWAY"))
        assertTrue(glm.matches(" from "))
        assertTrue(opus.matches("claude-opus"))
        assertFalse(opus.matches("haiku"))
        assertTrue(opus.matches(""))
    }

    private data class Call(val machine: String, val launcher: String, val refresh: Boolean)

    private class MemoryStore : AgentCreationStore {
        var operation: AgentStartOperation? = null
        val saved = mutableMapOf<String, AgentDefaults>()
        override suspend fun lastMachine(scope: String) = "first"
        override suspend fun defaults(scope: String, machine: String) = saved[machine] ?: AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) { saved[machine] = defaults }
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
    }

    private class Service : AgentCreationService {
        override val scope = "hub/pairing"
        var starts = 0
        var request: AgentStartRequest? = null
        var available = listOf(AgentLauncher("claude"))
        var model: ModelPurposeDetail? = null
        /** Absent launchers answer like an older hub (null). */
        var lists = emptyMap<String, LauncherModels>()
        var failure: Exception? = null
        var gate: CompletableDeferred<Unit>? = null
        val calls = mutableListOf<Call>()
        override suspend fun launchers(machineId: String) = available
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path.ifEmpty { "/code" })
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            starts++
            this.request = request
            return AgentStartResult.Created(Session(machineId = request.machineId, sessionId = "created"))
        }
        override suspend fun operation(operationId: String): AgentStartResult = AgentStartResult.Unknown
        override suspend fun newAgentModel(): ModelPurposeDetail? = model
        override suspend fun launcherModels(machineId: String, launcher: String, refresh: Boolean): LauncherModels? {
            calls += Call(machineId, launcher, refresh)
            gate?.await()
            failure?.let { throw it }
            return lists[launcher]
        }
    }
}
