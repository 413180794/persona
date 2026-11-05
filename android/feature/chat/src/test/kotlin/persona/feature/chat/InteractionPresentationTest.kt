package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Interaction
import persona.core.contracts.Machine
import org.junit.Assert.assertEquals
import org.junit.Test
import java.time.Instant

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class InteractionPresentationTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources
    @Test fun deadlineUsesRelativeUnitsAndExpiresAtTheBoundary() {
        val now = Instant.parse("2026-01-01T00:00:00Z")
        listOf(
            -1L to "已过期，回到终端处理",
            0L to "已过期，回到终端处理",
            1L to "即将过期",
            59L to "即将过期",
            60L to "1 分钟后过期",
            988L to "16 分钟后过期",
            3599L to "59 分钟后过期",
            3600L to "1 小时后过期",
            86400L to "1 天后过期",
        ).forEach { (seconds, expected) ->
            assertEquals(expected, interactionDeadline(now.plusSeconds(seconds), now))
        }
    }

    @Test fun sourceUsesTheMatchingMachineDisplayNameWithoutExposingItsId() {
        val request = Interaction("request", machineId = "machine-id", sessionTitle = "检查计划")
        val machine = Machine("machine-id", name = "开发电脑")
        assertEquals("检查计划 · 开发电脑", interactionSource(res, request, machine))
        assertEquals("检查计划 · 未知机器", interactionSource(res, request, null))
        assertEquals("检查计划 · 未知机器", interactionSource(res, request, machine.copy(name = "")))
        assertEquals("检查计划 · 未知机器", interactionSource(res, request, machine.copy(id = "another-id")))
    }
}
