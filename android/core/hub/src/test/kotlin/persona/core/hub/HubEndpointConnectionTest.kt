package persona.core.hub

import persona.core.contracts.Endpoint
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.api.EndpointOverrides
import java.net.InetAddress
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.*
import org.junit.Test

class HubEndpointConnectionTest {
    private fun connection(endpoints: List<Endpoint>, recheckMillis: Long = OkHttpHubConnection.RECHECK_MILLIS) = OkHttpHubConnection(
        HubConnectionConfig("test-hub", null, "test-token", endpoints, "test"), HubNetwork("offline", available = false),
        recheckMillis = recheckMillis)

    private suspend fun OkHttpHubConnection.awaitRoute(url: String, millis: Long = 5000) =
        withTimeout(millis) { route.first { it != null && endpointKey(it.endpoint.url) == endpointKey(url) } }!!

    private suspend fun OkHttpHubConnection.awaitOnline() = withTimeout(5000) { state.first { it == ConnectionState.Online } }

    @Test fun aManualCandidateIsUsedAndReportedInChinese() = runBlocking<Unit> {
        LoopbackHub().use { hub ->
            val dead = Endpoint("http://${InetAddress.getLoopbackAddress().hostAddress}:${closedPort()}/", "lan")
            connection(listOf(dead)).use { connection ->
                connection.updateOverrides(EndpointOverrides(manual = listOf(hub.url)))
                connection.updateNetwork(HubNetwork("network"))
                val route = connection.awaitRoute(hub.url)
                assertEquals(hub.url, route.endpoint.url)
                connection.awaitOnline()
                assertEquals(listOf(hub.url, dead.url), connection.candidates().map { it.url })
                val reported = connection.diagnostics.value.candidates.single { it.url == hub.url }
                assertEquals("已通过身份校验", (reported.result).zhAny())
            }
        }
    }

    @Test fun disabledCandidatesAreNeverProbedAndTheUsersOrderComesFirst() = runBlocking<Unit> {
        LoopbackHub().use { first ->
            LoopbackHub().use { second ->
                connection(listOf(first.endpoint("ipv6"), second.endpoint("name"))).use { connection ->
                    connection.updateOverrides(EndpointOverrides(disabled = setOf(first.url)))
                    connection.updateNetwork(HubNetwork("network"))
                    connection.awaitRoute(second.url)
                    assertEquals(0, first.probes.get())
                    assertEquals(0, first.sockets.get())
                }
                // By kind the ipv6 one goes first; the user's order puts the other one first, and it answers in its step.
                connection(listOf(first.endpoint("ipv6"), second.endpoint("name"))).use { connection ->
                    connection.updateOverrides(EndpointOverrides(order = listOf(second.url, first.url)))
                    connection.updateNetwork(HubNetwork("network"))
                    connection.awaitRoute(second.url)
                    assertEquals(0, first.probes.get())
                }
            }
        }
    }

    @Test fun theHubsListReplacesThePairingsButKeepsTheRouteChoices() = runBlocking<Unit> {
        LoopbackHub().use { listed ->
            LoopbackHub().use { disabled ->
                lateinit var advertised: String
                LoopbackHub(endpoints = { advertised }).use { manual ->
                    advertised = "[${listed.wire("ipv6")},${disabled.wire("ipv6")}]"
                    val pairing = Endpoint("https://hub.example.com:18662/", "name")
                    connection(listOf(pairing)).use { connection ->
                        val choices = EndpointOverrides(manual = listOf(manual.url), disabled = setOf(disabled.url), order = listOf(manual.url))
                        fun arranged() = arrangeEndpoints(connection.candidates(), connection.overrides.value).map { endpointKey(it.url) }
                        connection.updateOverrides(choices)
                        connection.updateNetwork(HubNetwork("network"))
                        connection.awaitRoute(manual.url)
                        // hello replaced the pairing's list without touching the choices
                        withTimeout(5000) { connection.advertised.first { it == setOf(listed.url, disabled.url) } }
                        assertEquals(listOf(manual.url, listed.url, disabled.url), connection.candidates().map { it.url })
                        assertEquals(listOf(manual.url, listed.url, disabled.url), arranged())
                        // An endpoints event replaces it again and reconnects: the user's order first, the hub's new
                        // https address before its http ones (the default rule), the disabled one untouched.
                        val firstSocket = manual.sockets.get()
                        advertised = "[${listed.wire("ipv6")},${disabled.wire("ipv6")},{\"url\":\"https://[2001:db8::1]:18662\",\"kind\":\"ipv6\"}]"
                        manual.advertise(advertised)
                        eventually { manual.sockets.get() > firstSocket }
                        connection.awaitRoute(manual.url)
                        assertEquals(choices, connection.overrides.value)
                        assertEquals(3, connection.advertised.value.size)
                        assertEquals(listOf(manual.url, "https://[2001:db8::1]:18662", listed.url, disabled.url), arranged())
                        assertEquals(0, disabled.probes.get())
                    }
                }
            }
        }
    }

    @Test fun reorderingOnlyChangesTheOrderAndTheNextRecheckFollowsIt() = runBlocking<Unit> {
        LoopbackHub(healthy = false).use { top ->
            LoopbackHub().use { used ->
                LoopbackHub().use { below ->
                    connection(listOf(top.endpoint("ipv6"), used.endpoint("name"), below.endpoint("lan")), recheckMillis = 3000).use { connection ->
                        connection.updateNetwork(HubNetwork("network"))
                        // top fails at once, so the next one wins; the one below it is never tried.
                        val initial = connection.awaitRoute(used.url)
                        connection.awaitOnline()
                        assertEquals(listOf(1, 1, 0), listOf(top.probes.get(), used.probes.get(), below.probes.get()))

                        // Dragged above the one in use, a working candidate is neither probed nor moved to: dragging only
                        // changes the order (the user's decision, 2026-11-02).
                        connection.updateOverrides(EndpointOverrides(order = listOf(below.url, top.url, used.url)))
                        delay(300)
                        assertSame(initial, connection.route.value)
                        assertEquals(listOf(1, 1, 0), listOf(top.probes.get(), used.probes.get(), below.probes.get()))

                        // The next periodic recheck goes by the new order and moves up to it.
                        connection.awaitRoute(below.url, millis = 10_000)
                        eventually { below.sockets.get() == 1 }
                        // Moving up is not a failure: nothing recorded, no backoff.
                        assertNull(connection.diagnostics.value.lastDisconnectReason)
                    }
                }
            }
        }
    }

    @Test fun changesThatPutNothingNewAboveTheOneInUseProbeNothing() = runBlocking<Unit> {
        LoopbackHub().use { first ->
            LoopbackHub().use { second ->
                LoopbackHub().use { third ->
                    LoopbackHub().use { manual ->
                        val hubs = listOf(first, second, third, manual)
                        connection(listOf(first.endpoint("ipv6"), second.endpoint("name"), third.endpoint("lan"))).use { connection ->
                            connection.updateNetwork(HubNetwork("network"))
                            val initial = connection.awaitRoute(first.url)
                            connection.awaitOnline()
                            suspend fun stays(change: EndpointOverrides) {
                                connection.updateOverrides(change)
                                delay(300)
                                assertSame(initial, connection.route.value)
                                assertEquals(listOf(1, 0, 0, 0), hubs.map { it.probes.get() })
                                assertEquals(1, first.sockets.get())
                            }
                            // Reordering below it, a manual candidate at the end, disabling one below, going back to the
                            // default order with the one in use still on top.
                            stays(EndpointOverrides(order = listOf(first.url, third.url, second.url)))
                            stays(EndpointOverrides(manual = listOf(manual.url), order = listOf(first.url, third.url, second.url, manual.url)))
                            stays(EndpointOverrides(manual = listOf(manual.url), disabled = setOf(second.url),
                                order = listOf(first.url, third.url, second.url, manual.url)))
                            stays(EndpointOverrides(manual = listOf(manual.url), disabled = setOf(second.url)))
                            stays(EndpointOverrides(manual = listOf(manual.url)))
                        }
                    }
                }
            }
        }
    }

    @Test fun disablingOrRemovingTheOneInUseStartsOverAtOnce() = runBlocking<Unit> {
        LoopbackHub().use { first ->
            LoopbackHub().use { second ->
                LoopbackHub().use { manual ->
                    connection(listOf(first.endpoint("ipv6"), second.endpoint("name"))).use { connection ->
                        val order = listOf(manual.url, first.url, second.url)
                        connection.updateOverrides(EndpointOverrides(manual = listOf(manual.url), order = order))
                        connection.updateNetwork(HubNetwork("network"))
                        connection.awaitRoute(manual.url)
                        connection.awaitOnline()
                        // Removed while in use: the next in the list.
                        connection.updateOverrides(EndpointOverrides(order = order))
                        connection.awaitRoute(first.url)
                        eventually { first.sockets.get() == 1 }
                        // Disabled while in use: the next one again.
                        connection.updateOverrides(EndpointOverrides(disabled = setOf(first.url), order = order))
                        connection.awaitRoute(second.url)
                        eventually { second.sockets.get() == 1 }
                        assertEquals(listOf(1, 1, 1), listOf(manual.sockets.get(), first.sockets.get(), second.sockets.get()))
                    }
                }
            }
        }
    }

    @Test fun thePeriodicRecheckProbesOnlyCandidatesAboveTheOneInUseAndNeverMovesDown() = runBlocking<Unit> {
        LoopbackHub(healthy = false).use { above ->
            LoopbackHub().use { used ->
                LoopbackHub().use { below ->
                    connection(listOf(above.endpoint("ipv6"), used.endpoint("name"), below.endpoint("lan")), recheckMillis = 200).use { connection ->
                        connection.updateNetwork(HubNetwork("network"))
                        val initial = connection.awaitRoute(used.url)
                        connection.awaitOnline()
                        // Every recheck probes the one above; the one in use and the one below, alive as they are, never.
                        eventually { above.probes.get() >= 4 }
                        assertSame(initial, connection.route.value)
                        assertEquals(listOf(1, 1, 0), listOf(used.probes.get(), used.sockets.get(), below.probes.get()))
                        // Once the one above answers, the connection moves up to it.
                        above.healthy = true
                        connection.awaitRoute(above.url)
                        val moved = connection.route.value
                        connection.awaitOnline()
                        // On top nothing is probed any more, and it never moves down by itself.
                        val probes = listOf(above, used, below).map { it.probes.get() }
                        delay(1000)
                        assertSame(moved, connection.route.value)
                        assertEquals(probes, listOf(above, used, below).map { it.probes.get() })
                        assertEquals(listOf(1, 1, 0), listOf(above.sockets.get(), used.sockets.get(), below.sockets.get()))
                        assertNull(connection.diagnostics.value.lastDisconnectReason)
                    }
                }
            }
        }
    }

    @Test fun aHigherCandidateSlowerThanItsStepDoesNotMakeTheConnectionStartOverAtEveryRecheck() = runBlocking<Unit> {
        LoopbackHub().use { slow ->
            LoopbackHub().use { fast ->
                // Alive, but it answers only after the next one has started and won the race.
                slow.answerMillis = HubRouteSelector.STEP_MILLIS + 1500
                connection(listOf(slow.endpoint("ipv6"), fast.endpoint("name")), recheckMillis = 200).use { connection ->
                    connection.updateNetwork(HubNetwork("network"))
                    val initial = connection.awaitRoute(fast.url)
                    connection.awaitOnline()
                    // Each recheck gives it the step it has in the race and gives up: the race would pick the same route.
                    eventually(10_000) { slow.probes.get() >= 4 }
                    assertSame(initial, connection.route.value)
                    assertEquals(listOf(0, 1), listOf(slow.sockets.get(), fast.sockets.get()))
                    val reported = connection.diagnostics.value.candidates.single { it.url == slow.paired }
                    assertEquals("连接超时", (reported.result).zhAny())
                }
            }
        }
    }

    @Test fun withoutARouteNewChoicesApplyAtOnceInsteadOfAfterTheBackoff() = runBlocking<Unit> {
        LoopbackHub(healthy = false).use { failing ->
            LoopbackHub().use { working ->
                connection(listOf(failing.endpoint("lan"))).use { connection ->
                    connection.updateNetwork(HubNetwork("network"))
                    // After the second failure the next attempt is at least 1.6 s away (backoff 2 s × 0.8–1.0).
                    eventually { failing.probes.get() >= 2 }
                    delay(100)
                    assertNull(connection.route.value)
                    connection.updateOverrides(EndpointOverrides(manual = listOf(working.url)))
                    connection.awaitRoute(working.url, millis = 1200)
                }
            }
        }
    }
}
