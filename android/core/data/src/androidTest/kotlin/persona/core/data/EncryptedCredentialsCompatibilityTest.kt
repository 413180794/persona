package persona.core.data

import android.content.Context
import android.content.ContextWrapper
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import com.google.crypto.tink.Aead
import com.google.crypto.tink.BinaryKeysetWriter
import com.google.crypto.tink.KeysetHandle
import com.google.crypto.tink.aead.AeadConfig
import com.google.crypto.tink.aead.AeadKeyTemplates
import com.google.crypto.tink.integration.android.AndroidKeystoreKmsClient
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.UUID
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import persona.core.contracts.ProtocolCompatibility

@Suppress("DEPRECATION")
@RunWith(AndroidJUnit4::class)
class EncryptedCredentialsCompatibilityTest {
    @Test fun oldAndCurrentKeystoreKeysKeepExistingPairings() = runBlocking {
        AeadConfig.register()
        // Both aliases exist before reading either wrapped keyset.
        val masters = ProtocolCompatibility.pairingMasterKeyUris.associateWith {
            AndroidKeystoreKmsClient.getOrGenerateNewAeadKey(it)
        }
        for ((_, master) in masters) {
            fixture { context ->
                val root = File(context.noBackupFilesDir, "hub-data").apply { mkdirs() }
                val handle = KeysetHandle.generateNew(AeadKeyTemplates.AES256_GCM)
                val wrapped = ByteArrayOutputStream().use {
                    handle.write(BinaryKeysetWriter.withOutputStream(it), master)
                    it.toByteArray()
                }
                val keyset = File(root, "keyset.enc").apply { writeBytes(wrapped) }
                val data = handle.getPrimitive(Aead::class.java)
                val document = """{"version":1,"hubId":"retained-hub","generation":"retained-generation","discarding":false}"""
                val ciphertext = data.encrypt(document.toByteArray(), "active.enc".toByteArray())
                File(root, "active.enc").writeBytes(ciphertext)
                repeat(2) {
                    val job = SupervisorJob()
                    try {
                        val active = EncryptedCredentials(context).active(CoroutineScope(job + Dispatchers.IO)).data.first()
                        assertEquals("retained-hub", active.hubId)
                        assertEquals("retained-generation", active.generation)
                        assertArrayEquals(wrapped, keyset.readBytes())
                        assertArrayEquals(ciphertext, File(root, "active.enc").readBytes())
                    } finally { job.cancelAndJoin() }
                }
                assertTrue(keyset.renameTo(File(root, "keyset.enc.bak")))
                val job = SupervisorJob()
                try {
                    assertEquals("retained-hub", EncryptedCredentials(context)
                        .active(CoroutineScope(job + Dispatchers.IO)).data.first().hubId)
                    assertArrayEquals(wrapped, keyset.readBytes())
                } finally { job.cancelAndJoin() }
            }
        }
    }

    @Test fun newInstallKeepsPairingAfterRestart() = runBlocking {
        fixture { context ->
            val firstJob = SupervisorJob()
            try {
                EncryptedCredentials(context).active(CoroutineScope(firstJob + Dispatchers.IO))
                    .updateData { ActivePairing(hubId = "new-hub", generation = "new-generation") }
            } finally { firstJob.cancelAndJoin() }
            val nextJob = SupervisorJob()
            try {
                assertEquals("new-hub", EncryptedCredentials(context)
                    .active(CoroutineScope(nextJob + Dispatchers.IO)).data.first().hubId)
            } finally { nextJob.cancelAndJoin() }
        }
    }

    private suspend fun fixture(block: suspend (Context) -> Unit) {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val directory = File(base.cacheDir, "pairing-compat-${UUID.randomUUID()}").apply { mkdirs() }
        val context = object : ContextWrapper(base) { override fun getNoBackupFilesDir() = directory }
        try { block(context) } finally { check(directory.deleteRecursively()) }
    }
}
