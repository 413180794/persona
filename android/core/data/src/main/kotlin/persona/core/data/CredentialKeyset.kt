package persona.core.data

import com.google.crypto.tink.Aead
import com.google.crypto.tink.BinaryKeysetReader
import com.google.crypto.tink.KeysetHandle
import java.io.IOException
import java.security.GeneralSecurityException
import persona.core.contracts.ProtocolCompatibility

/** Reads existing keysets without changing their wrapping key or persisted ciphertext. */
internal object CredentialKeyset {
    fun read(wrapped: ByteArray, master: (String) -> Aead): KeysetHandle {
        val failure = GeneralSecurityException("Cannot unlock existing pairing keyset; original data retained")
        for (uri in ProtocolCompatibility.pairingMasterKeyUris) {
            try {
                return KeysetHandle.read(BinaryKeysetReader.withBytes(wrapped), master(uri))
            } catch (cause: GeneralSecurityException) {
                failure.addSuppressed(cause)
            } catch (cause: IOException) {
                failure.addSuppressed(cause)
            }
        }
        throw failure
    }
}
