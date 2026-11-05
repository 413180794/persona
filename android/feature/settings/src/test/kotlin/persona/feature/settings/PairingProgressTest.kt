package persona.feature.settings

import persona.core.contracts.api.PairingProgress
import persona.core.contracts.api.PairingStage
import persona.core.designsystem.StepState
import org.junit.Assert.assertEquals
import org.junit.Test

class PairingProgressTest {
    @Test fun failureMarksCurrentStageAndKeepsUnreportedStagesPending() {
        val steps = pairingSteps(listOf(
            PairingProgress(PairingStage.Parse),
            PairingProgress(PairingStage.ExchangeCredentials),
        ), running = false, failed = true) { it.name }
        assertEquals(StepState.Complete, steps[PairingStage.Parse.ordinal].state)
        assertEquals(StepState.Pending, steps[PairingStage.Probe.ordinal].state)
        assertEquals(StepState.Pending, steps[PairingStage.VerifyIdentity.ordinal].state)
        assertEquals(StepState.Failed, steps[PairingStage.ExchangeCredentials.ordinal].state)
        assertEquals(StepState.Pending, steps[PairingStage.LoadMachines.ordinal].state)
        assertEquals(StepState.Pending, steps[PairingStage.Complete.ordinal].state)
    }

    @Test fun completionWaitsForMachineRefreshAfterServiceFinishes() {
        val progress = listOf(
            PairingProgress(PairingStage.Parse),
            PairingProgress(PairingStage.Complete),
            PairingProgress(PairingStage.LoadMachines),
        )
        val loading = pairingSteps(progress, running = true, failed = false) { it.name }
        assertEquals(StepState.Active, loading[PairingStage.LoadMachines.ordinal].state)
        assertEquals(StepState.Pending, loading[PairingStage.Complete.ordinal].state)
        val completed = pairingSteps(progress.filterNot { it.stage == PairingStage.Complete } +
            PairingProgress(PairingStage.Complete), running = false, failed = false) { it.name }
        assertEquals(StepState.Complete, completed[PairingStage.LoadMachines.ordinal].state)
        assertEquals(StepState.Complete, completed[PairingStage.Complete.ordinal].state)
    }
}
