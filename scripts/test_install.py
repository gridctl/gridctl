"""Exercise installer downloads with real curl and a local HTTP server."""

import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import subprocess
import threading
import unittest


INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"
ARCHIVE = "gridctl_test.tar.gz"
PAYLOAD = b"release archive test content\n"


class InstallerDownloadTests(unittest.TestCase):
    def run_download(self, failures, corrupt_checksum=False):
        counts = {}

        class Handler(BaseHTTPRequestHandler):
            def do_HEAD(self):
                self.respond()

            def do_GET(self):
                self.respond()

            def respond(self):
                key = (self.command, self.path)
                counts[key] = counts.get(key, 0) + 1
                status, attempts = failures.get(key, (200, 0))
                status = status if counts[key] <= attempts else 200
                payload = PAYLOAD
                if self.path == "/checksums.txt":
                    digest = hashlib.sha256(b"wrong" if corrupt_checksum else PAYLOAD).hexdigest()
                    payload = f"{digest}  {ARCHIVE}\n".encode()
                self.send_response(status)
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                if self.command != "HEAD":
                    self.wfile.write(payload)

            def log_message(self, *_args):
                pass

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}"
            # Load the actual shell functions without running the CLI entry point.
            source, entrypoint = INSTALLER.read_text().rsplit('\nmain "$@"', 1)
            self.assertEqual(entrypoint.strip(), "")
            env = dict(os.environ, ARCHIVE=ARCHIVE, ARCHIVE_URL=f"{base}/{ARCHIVE}",
                       CHECKSUMS_URL=f"{base}/checksums.txt", OS="linux", ARCH="amd64",
                       NO_COLOR="1", NO_PROXY="127.0.0.1", no_proxy="127.0.0.1")
            result = subprocess.run(
                ["sh", "-c", source + "\ndownload\nverify_checksum\n"],
                env=env, capture_output=True, text=True, timeout=30,
            )
            return result, counts
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_transient_504_recovers_at_each_download_stage(self):
        keys = [("HEAD", f"/{ARCHIVE}"), ("GET", f"/{ARCHIVE}"),
                ("GET", "/checksums.txt")]
        result, counts = self.run_download({key: (504, 1) for key in keys})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("checksum matches", result.stdout)
        for key in keys:
            self.assertEqual(counts[key], 2)

    def test_persistent_504_exhausts_retries(self):
        key = ("GET", f"/{ARCHIVE}")
        result, counts = self.run_download({key: (504, 100)})
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(counts[key], 4)
        self.assertNotIn(("GET", "/checksums.txt"), counts)

    def test_missing_artifact_is_not_retried(self):
        key = ("HEAD", f"/{ARCHIVE}")
        result, counts = self.run_download({key: (404, 100)})
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(counts, {key: 1})

    def test_recovered_download_still_requires_valid_checksum(self):
        key = ("GET", f"/{ARCHIVE}")
        result, counts = self.run_download({key: (504, 1)}, corrupt_checksum=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(counts[key], 2)
        self.assertIn("Checksum verification failed", result.stderr)


if __name__ == "__main__":
    unittest.main()
