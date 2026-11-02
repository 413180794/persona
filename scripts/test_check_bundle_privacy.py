from contextlib import redirect_stdout
import io
import ipaddress
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parent))
import check_bundle_privacy as cbp


DEPENDENCIES = {"golang.org/x/crypto", "tailscale.com", "github.com/tailscale/web-client-prebuilt"}
NETWORKS = tuple(ipaddress.ip_network((address, prefix)) for address, prefix in (
    (0x0A000000, 8), (0xAC100000, 12), (0xC0A80000, 16),
))
METADATA = str(ipaddress.ip_address(0xA9FEA9FE))
CONTACT = '<a href="mailto:support+webclient@' + 'tailscale.com" class="link">contact us</a>'
SSH_REQUEST = "ping@" + "openssh.com"
EMAIL = "person@" + "example.dev"


class PublicConstantTests(unittest.TestCase):
    def kinds(self, text, words=(), dependencies=DEPENDENCIES):
        return cbp.text_kinds(text, words, whole_short=True, dependencies=dependencies)

    def test_only_the_observed_openssh_request_is_classified(self):
        self.assertEqual(self.kinds("failed to unmarshal " + SSH_REQUEST + " message: %w"), set())
        for text in ("person@" + "openssh.com", "prefix" + SSH_REQUEST, SSH_REQUEST + ".cn"):
            with self.subTest(text=text):
                self.assertIn("email", self.kinds(text))

    def test_exact_upstream_contact_link_not_the_entire_domain(self):
        self.assertEqual(self.kinds(CONTACT), set())
        for text in ("person@" + "tailscale.com", "support+webclient@" + "tailscale.com",
                     CONTACT.replace("tailscale.com", "tailscale.com.cn"),
                     CONTACT.replace("contact us", "another account")):
            with self.subTest(text=text):
                self.assertIn("email", self.kinds(text))

    def test_full_rfc1918_networks_are_not_host_addresses(self):
        for network in NETWORKS:
            with self.subTest(network=str(network)):
                self.assertEqual(self.kinds("constant" + str(network) + "nextConstant"), set())
                self.assertIn("private IP", self.kinds(str(network.network_address)))
                self.assertIn("private IP", self.kinds(str(network.network_address) + "/24"))
                self.assertIn("private IP", self.kinds(str(network.network_address) + "/" + str(network.prefixlen) + "0"))
                self.assertIn("private IP", self.kinds(str(network.network_address + 17) + "/24"))

    def test_adjacent_cidrs_in_go_rodata(self):
        link_local = str(ipaddress.ip_network((0xA9FE0000, 16)))
        self.assertEqual(self.kinds(str(NETWORKS[2]) + link_local + "profiles_count"), set())
        self.assertIn("private IP", self.kinds(str(NETWORKS[2]) + "999nextConstant"))

    def test_cloud_metadata_requires_a_known_context(self):
        for text in ("constanthttp://" + METADATA + "/latest/api/token", "http://" + METADATA,
                     METADATA + str(ipaddress.ip_address(int(ipaddress.ip_address(METADATA)) - 1)) + "Metadata-Flavor"):
            with self.subTest(text=text):
                self.assertEqual(self.kinds(text), set())
        for text in (METADATA, "hub=" + METADATA, "https://" + METADATA,
                     "http://" + str(ipaddress.ip_address(int(ipaddress.ip_address(METADATA)) + 1))):
            with self.subTest(text=text):
                self.assertIn("private IP", self.kinds(text))

    def test_classification_requires_the_corresponding_dependency(self):
        samples = [(SSH_REQUEST, "golang.org/x/crypto", "email"),
                   (CONTACT, "github.com/tailscale/web-client-prebuilt", "email"),
                   (str(NETWORKS[0]), "tailscale.com", "private IP"),
                   ("http://" + METADATA, "tailscale.com", "private IP")]
        for text, dependency, kind in samples:
            with self.subTest(dependency=dependency, kind=kind):
                self.assertIn(kind, self.kinds(text, dependencies=()))
                self.assertIn(kind, self.kinds(text, dependencies=DEPENDENCIES - {dependency}))
                self.assertIn(kind, cbp.text_kinds(text, ()))

    def test_registered_values_are_never_removed(self):
        for text, word in ((SSH_REQUEST, SSH_REQUEST),
                           (CONTACT, "support+webclient@" + "tailscale.com"),
                           (str(NETWORKS[0]), str(NETWORKS[0].network_address)),
                           ("http://" + METADATA, METADATA)):
            with self.subTest(text=text):
                self.assertIn(f"private value ({len(word)} chars)", self.kinds(text, words=(word,)))

    def test_unrelated_private_data_next_to_public_constants_still_fails(self):
        address = str(NETWORKS[0].network_address + 17)
        text = f"{SSH_REQUEST} {CONTACT} {NETWORKS[0]} http://{METADATA} {EMAIL} {address} secret-placeholder"
        self.assertEqual(self.kinds(text, words=("secret-placeholder",)),
                         {"email", "private IP", "private value (18 chars)"})

    def test_short_word_matching_remains_unchanged(self):
        self.assertEqual(self.kinds("foobar", words=("foo",)), set())
        self.assertEqual(self.kinds("FOO foo", words=("foo",)), {"private value (3 chars)"})
        self.assertEqual(cbp.text_kinds("foobar", ("foo",)), {"private value (3 chars)"})


class GoDependenciesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="bundle-privacy-test-")
        self.addCleanup(self.temp.cleanup)
        self.sums = Path(self.temp.name) / "go.sum"
        self.sums.write_text("golang.org/x/crypto v1.0.0 h1:placeholder\n")
        self.build_info = "\tdep\tgolang.org/x/crypto\tv1.0.0\th1:placeholder\n\tbuild\t-tags=public\n"

    def dependencies(self, text, returncode=0):
        result = subprocess.CompletedProcess([], returncode, stdout=text)
        with patch.object(cbp.subprocess, "run", return_value=result):
            return cbp.go_dependencies("binary", str(self.sums))

    def test_checksum_and_public_build_are_required(self):
        self.assertEqual(self.dependencies(self.build_info), {"golang.org/x/crypto"})
        self.assertEqual(self.dependencies(self.build_info.replace("h1:placeholder", "h1:other")), set())
        self.assertEqual(self.dependencies(self.build_info.replace("v1.0.0", "v2.0.0")), set())
        self.assertEqual(self.dependencies(self.build_info.replace("-tags=public", "-tags=debug")), set())

    def test_module_replacements_are_not_trusted(self):
        replaced = self.build_info.replace("\n\tbuild", "\n\t=>\t./local-module\t(devel)\n\tbuild")
        self.assertEqual(self.dependencies(replaced), set())
        no_sum = self.build_info.replace("\th1:placeholder", "")
        self.assertEqual(self.dependencies(no_sum), set())

    def test_unrelated_replacements_do_not_remove_verified_dependencies(self):
        info = self.build_info + "\tdep\tother.module\tv1.0.0\n\t=>\t./local-module\t(devel)\n"
        self.assertEqual(self.dependencies(info), {"golang.org/x/crypto"})

    def test_tool_failures_and_missing_sums_fail_closed(self):
        self.assertEqual(self.dependencies(self.build_info, returncode=1), set())
        for error in (FileNotFoundError(), subprocess.TimeoutExpired("go", 10)):
            with self.subTest(error=type(error).__name__):
                with patch.object(cbp.subprocess, "run", side_effect=error):
                    self.assertEqual(cbp.go_dependencies("binary", str(self.sums)), set())
        self.sums.unlink()
        self.assertEqual(cbp.go_dependencies("binary", str(self.sums)), set())


class BundleScanIntegrationTests(unittest.TestCase):
    def scan(self, text, words=(), public=True, config=None):
        with tempfile.TemporaryDirectory(prefix="bundle-privacy-test-") as directory:
            (Path(directory) / "binary").write_bytes(text.encode())
            output = io.StringIO()
            args = ["check_bundle_privacy.py"] + (["--whole-short"] if public else []) + [directory]
            with patch.object(sys, "argv", args), patch.object(cbp.pc, "module_paths", return_value=None), \
                    patch.object(cbp.pc, "terms", return_value=(words, set())), \
                    patch.object(cbp.jarvis_config, "load", return_value=config or {}), \
                    patch.object(cbp, "go_dependencies", return_value=DEPENDENCIES), redirect_stdout(output):
                try:
                    cbp.main()
                except SystemExit as error:
                    return error.code, output.getvalue()
            return 0, output.getvalue()

    def test_public_binary_scan_classifies_verified_constants(self):
        text = f"{SSH_REQUEST}\n{CONTACT}\n{NETWORKS[0]}\nhttp://{METADATA}"
        code, output = self.scan(text)
        self.assertEqual(code, 0, output)
        self.assertIn("check_bundle_privacy: clean", output)

    def test_registered_values_still_fail_and_are_not_logged(self):
        for text, value in ((CONTACT, "support+webclient@" + "tailscale.com"),
                            ("http://" + METADATA, METADATA)):
            with self.subTest(value=value):
                code, output = self.scan(text, words=(value,))
                self.assertNotEqual(code, 0)
                self.assertIn("private value", output)
                self.assertNotIn(value, output)

    def test_unknown_private_data_still_fails_and_is_not_logged(self):
        address = str(NETWORKS[1].network_address + 17)
        code, output = self.scan(CONTACT + "\n" + EMAIL + "\n" + address)
        self.assertNotEqual(code, 0)
        self.assertIn("email", output)
        self.assertIn("private IP", output)
        self.assertNotIn(EMAIL, output)
        self.assertNotIn(address, output)

    def test_app_accepts_only_the_exact_configured_public_relay_url(self):
        relay = "https://relay.example.test/relay"
        config = {"app": {"push_relay": relay}}
        words = ("relay.example.test", relay)
        for text in (relay, '<string>' + relay + '</string>'):
            with self.subTest(text=text):
                code, output = self.scan(text, words=words, public=False, config=config)
                self.assertEqual(code, 0, output)
        for text in (relay + "/private", relay + "?token=placeholder", relay + "#context",
                     relay + ".other", "relay.example.test", "person@relay.example.test",
                     "https://user@relay.example.test/relay", relay + "\nrelay.example.test"):
            with self.subTest(text=text):
                code, output = self.scan(text, words=words, public=False, config=config)
                self.assertNotEqual(code, 0)
                self.assertIn("private value", output)
        code, _ = self.scan(relay, words=words, public=True, config=config)
        self.assertNotEqual(code, 0)

    def test_non_public_or_invalid_relay_urls_remain_private(self):
        for relay in ("http://relay.example.test/relay", "https://user:password@relay.example.test/relay",
                      "https://relay.example.test/relay?token=placeholder", "https://relay.example.test/other",
                      "https://localhost/relay", "https://203.0.113.17/relay", "https://[invalid/relay",
                      "https://relay.example.test:8443/relay"):
            with self.subTest(relay=relay):
                code, output = self.scan(relay, words=(relay,), public=False, config={"app": {"push_relay": relay}})
                self.assertNotEqual(code, 0)
                self.assertIn("private value", output)
        for relay in (None, [], {}):
            self.assertEqual(cbp.remove_public_relay_url("private-placeholder", relay), "private-placeholder")

    def test_published_binary_accepts_only_the_exact_public_download_url(self):
        site = "https://site.example.test"
        url = site + "/dl"
        words = ("site.example.test", site)
        for config in ({"publish": {"site_url": site}}, {"publish": {"site_url": site + "/"}}):
            code, output = self.scan(url, words=words, config=config)
            self.assertEqual(code, 0, output)
        config = {"publish": {"site_url": site}}
        for text in (site, url + "/private", url + "?token=placeholder", url + ".other", "site.example.test",
                     "person@site.example.test", "https://user@site.example.test/dl", url + "\nsite.example.test"):
            with self.subTest(text=text):
                code, output = self.scan(text, words=words, config=config)
                self.assertNotEqual(code, 0)
                self.assertIn("private value", output)
                self.assertNotIn("site.example.test", output)
        code, _ = self.scan(url, words=words, public=False, config=config)  # app bundles do not carry it
        self.assertNotEqual(code, 0)
        for bad in ("http://site.example.test", "https://203.0.113.17", "https://site.example.test:8443"):
            with self.subTest(site=bad):
                code, _ = self.scan(bad + "/dl", words=(bad + "/dl",), config={"publish": {"site_url": bad}})
                self.assertNotEqual(code, 0)

    def test_app_mode_stays_strict(self):
        code, output = self.scan(CONTACT + "\n" + str(NETWORKS[0]), public=False)
        self.assertNotEqual(code, 0)
        self.assertIn("email", output)
        self.assertIn("private IP", output)


if __name__ == "__main__":
    unittest.main()
