package persona.core.data

import android.content.Context
import android.util.AtomicFile
import androidx.datastore.core.CorruptionException
import androidx.datastore.core.DataStore
import androidx.datastore.core.DataStoreFactory
import androidx.datastore.core.Serializer
import androidx.datastore.core.handlers.ReplaceFileCorruptionHandler
import androidx.datastore.tink.AeadSerializer
import com.google.crypto.tink.Aead
import com.google.crypto.tink.BinaryKeysetReader
import com.google.crypto.tink.BinaryKeysetWriter
import com.google.crypto.tink.KeysetHandle
import com.google.crypto.tink.aead.AeadConfig
import com.google.crypto.tink.aead.AeadKeyTemplates
import com.google.crypto.tink.integration.android.AndroidKeystoreKmsClient
import java.io.File
import java.io.InputStream
import java.io.OutputStream
import kotlinx.coroutines.CoroutineScope
import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json

@Serializable
data class HubCredentials(
    val hubAddress: String,
    val hubId: String,
    val fp: String?,
    val candidateAddresses: List<String>,
    val deviceToken: String,
    val deviceId: String? = null,
) {
    override fun toString(): String = "HubCredentials(<redacted>)"
}

@Serializable
internal data class CredentialDocument(val version: Int = 1, val credentials: HubCredentials? = null)

/** Only the active scope pointer and wrapped keyset live outside the scoped directories. */
@Serializable
internal data class ActivePairing(
    val version: Int = 1,
    val hubId: String? = null,
    val generation: String? = null,
    val discarding: Boolean = false,
)

internal class EncryptedCredentials(context: Context) {
    val root = File(context.noBackupFilesDir, "hub-data").apply { mkdirs() }
    private val aead: Aead = createAead()

    fun active(scope: CoroutineScope): DataStore<ActivePairing> = store(
        File(root, "active.enc"), scope, ActivePairing(), ActivePairing.serializer(), { it.version },
    )

    fun credentials(path: String, scope: CoroutineScope): DataStore<CredentialDocument> = store(
        File(root, "$path/credentials.enc"), scope, CredentialDocument(), CredentialDocument.serializer(), { it.version },
    )

    /**
     * The hub groups (HubGroupRegistry.kt). Like credentials, a file that cannot be read is kept and reported
     * instead of being replaced: losing it would orphan every pairing.
     */
    fun groups(scope: CoroutineScope): DataStore<GroupsDocument> = store(
        groupsFile, scope, GroupsDocument(), GroupsDocument.serializer(), { it.version },
    )

    val groupsFile: File get() = File(root, "groups.enc")

    /** Route choices are optional: data that cannot be read falls back to "Automatic" instead of blocking the pairing. */
    fun endpoints(path: String, scope: CoroutineScope): DataStore<EndpointOverridesDocument> = store(
        File(root, "$path/endpoints.enc"), scope, EndpointOverridesDocument(), EndpointOverridesDocument.serializer(),
        { it.version }, ReplaceFileCorruptionHandler { EndpointOverridesDocument() },
    )

    private fun <T> store(
        file: File, scope: CoroutineScope, default: T, serializer: KSerializer<T>, version: (T) -> Int,
        corruption: ReplaceFileCorruptionHandler<T>? = null,
    ): DataStore<T> = DataStoreFactory.create(
        serializer = AeadSerializer(aead, VersionedSerializer(default, serializer, version),
            file.relativeTo(root).path.toByteArray(Charsets.UTF_8)),
        corruptionHandler = corruption,
        scope = scope,
        produceFile = { file.apply { parentFile!!.mkdirs() } },
    )

    @Suppress("DEPRECATION")
    private fun createAead(): Aead {
        AeadConfig.register()
        val keyset = AtomicFile(File(root, "keyset.enc"))
        val uri = "android-keystore://persona.hub-data.master"
        val handle = if (keyset.baseFile.exists()) {
            // A missing or unusable Keystore key must fail closed, preserving the original ciphertext.
            val master = AndroidKeystoreKmsClient().getAead(uri)
            keyset.openRead().use { KeysetHandle.read(BinaryKeysetReader.withInputStream(it), master) }
        } else {
            val master = AndroidKeystoreKmsClient.getOrGenerateNewAeadKey(uri)
            val generated = KeysetHandle.generateNew(AeadKeyTemplates.AES256_GCM)
            val output = keyset.startWrite()
            try {
                generated.write(BinaryKeysetWriter.withOutputStream(output), master)
                keyset.finishWrite(output)
            } catch (failure: Exception) {
                keyset.failWrite(output)
                throw failure
            }
            generated
        }
        return handle.getPrimitive(Aead::class.java)
    }
}

internal class VersionedSerializer<T>(
    override val defaultValue: T,
    private val serializer: KSerializer<T>,
    private val version: (T) -> Int,
) : Serializer<T> {
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    override suspend fun readFrom(input: InputStream): T {
        val value = try {
            json.decodeFromString(serializer, input.readBytes().toString(Charsets.UTF_8))
        } catch (failure: SerializationException) {
            throw CorruptionException("Cannot read encrypted pairing data", failure)
        }
        checkVersion(value)
        return value
    }

    override suspend fun writeTo(t: T, output: OutputStream) {
        checkVersion(t)
        output.write(json.encodeToString(serializer, t).toByteArray(Charsets.UTF_8))
    }

    private fun checkVersion(value: T) {
        if (version(value) != 1) throw CorruptionException("Unsupported pairing storage version; original data retained")
    }
}
