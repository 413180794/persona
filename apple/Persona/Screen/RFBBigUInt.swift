import Foundation

/// Unsigned big integers just big enough for Apple Remote Desktop's Diffie-Hellman sign-in (RFBAuth): an odd modulus
/// of a few thousand bits at most, numbers in and out as big-endian bytes, and modular exponentiation by Montgomery
/// multiplication (CIOS), so no general division is needed.
nonisolated struct RFBBigUInt: Equatable, Sendable {
    /// Little-endian 32-bit limbs.
    var limbs: [UInt32]

    init(limbs: [UInt32]) { self.limbs = limbs.isEmpty ? [0] : limbs }

    /// From big-endian bytes.
    init(bytes: [UInt8]) {
        var out: [UInt32] = []
        var i = bytes.count
        while i > 0 {
            var limb: UInt32 = 0
            for k in Swift.max(0, i - 4)..<i { limb = limb << 8 | UInt32(bytes[k]) }
            out.append(limb)
            i -= 4
        }
        self.init(limbs: out)
    }

    /// Big-endian, left-padded with zeros (or cut to its low bytes) to count.
    func bytes(count: Int) -> [UInt8] {
        var out = [UInt8](repeating: 0, count: count)
        for (i, limb) in limbs.enumerated() {
            for k in 0..<4 {
                let at = count - 1 - (i * 4 + k)
                if at >= 0 { out[at] = UInt8(truncatingIfNeeded: limb >> (8 * UInt32(k))) }
            }
        }
        return out
    }

    var bitWidth: Int {
        guard let top = limbs.lastIndex(where: { $0 != 0 }) else { return 0 }
        return top * 32 + (32 - limbs[top].leadingZeroBitCount)
    }

    func bit(_ i: Int) -> Bool { i / 32 < limbs.count && limbs[i / 32] >> UInt32(i % 32) & 1 == 1 }

    /// base^exp mod m, for an odd m.
    static func modPow(_ base: RFBBigUInt, _ exp: RFBBigUInt, _ m: RFBBigUInt) -> RFBBigUInt {
        let n = m.limbs.count
        precondition(m.limbs[0] & 1 == 1, "Montgomery needs an odd modulus")
        let mod = m.limbs
        // m' = -m⁻¹ mod 2³², by Newton's iteration.
        var inv: UInt32 = 1
        for _ in 0..<5 { inv = inv &* (2 &- mod[0] &* inv) }
        let mPrime = 0 &- inv

        func geq(_ a: [UInt32], _ b: [UInt32]) -> Bool {
            for i in stride(from: Swift.max(a.count, b.count) - 1, through: 0, by: -1) {
                let x = i < a.count ? a[i] : 0, y = i < b.count ? b[i] : 0
                if x != y { return x > y }
            }
            return true
        }
        func subtract(_ a: inout [UInt32], _ b: [UInt32]) {
            var borrow: Int64 = 0
            for i in 0..<a.count {
                var d = Int64(a[i]) - Int64(i < b.count ? b[i] : 0) - borrow
                borrow = d < 0 ? 1 : 0
                if d < 0 { d += 1 << 32 }
                a[i] = UInt32(d)
            }
        }
        /// x mod m, a bit at a time from the top.
        func reduce(_ x: RFBBigUInt) -> [UInt32] {
            var r = [UInt32](repeating: 0, count: n + 1)
            for i in stride(from: x.bitWidth - 1, through: 0, by: -1) {
                var carry: UInt32 = x.bit(i) ? 1 : 0
                for k in 0..<r.count {
                    let next = r[k] >> 31
                    r[k] = r[k] << 1 | carry
                    carry = next
                }
                if geq(r, mod) { subtract(&r, mod) }
            }
            return Array(r[0..<n])
        }
        /// (2^bits) mod m.
        func power2(_ bits: Int) -> [UInt32] {
            var r = [UInt32](repeating: 0, count: n + 1)
            r[0] = 1
            if geq(r, mod) { subtract(&r, mod) }
            for _ in 0..<bits {
                var carry: UInt32 = 0
                for k in 0..<r.count {
                    let next = r[k] >> 31
                    r[k] = r[k] << 1 | carry
                    carry = next
                }
                if geq(r, mod) { subtract(&r, mod) }
            }
            return Array(r[0..<n])
        }
        /// a·b·R⁻¹ mod m (a, b < m).
        func mul(_ a: [UInt32], _ b: [UInt32]) -> [UInt32] {
            var t = [UInt32](repeating: 0, count: n + 2)
            for i in 0..<n {
                var c: UInt64 = 0
                let ai = UInt64(a[i])
                for j in 0..<n {
                    let s = UInt64(t[j]) + ai * UInt64(b[j]) + c
                    t[j] = UInt32(truncatingIfNeeded: s)
                    c = s >> 32
                }
                var s = UInt64(t[n]) + c
                t[n] = UInt32(truncatingIfNeeded: s)
                t[n + 1] = UInt32(s >> 32)
                let q = UInt64(t[0] &* mPrime)
                s = UInt64(t[0]) + q * UInt64(mod[0])
                c = s >> 32
                for j in 1..<n {
                    s = UInt64(t[j]) + q * UInt64(mod[j]) + c
                    t[j - 1] = UInt32(truncatingIfNeeded: s)
                    c = s >> 32
                }
                s = UInt64(t[n]) + c
                t[n - 1] = UInt32(truncatingIfNeeded: s)
                t[n] = t[n + 1] + UInt32(s >> 32)
                t[n + 1] = 0
            }
            var r = Array(t[0...n])
            if geq(r, mod) { subtract(&r, mod) }
            return Array(r[0..<n])
        }

        let rr = power2(64 * n) // R² mod m, R = 2^(32n)
        var acc = power2(32 * n) // 1 in Montgomery form
        let b = mul(reduce(base), rr)
        for i in stride(from: exp.bitWidth - 1, through: 0, by: -1) {
            acc = mul(acc, acc)
            if exp.bit(i) { acc = mul(acc, b) }
        }
        var one = [UInt32](repeating: 0, count: n)
        one[0] = 1
        return RFBBigUInt(limbs: mul(acc, one))
    }
}
