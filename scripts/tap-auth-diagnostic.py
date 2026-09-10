"""Compare tap authentication without writes or credential-bearing diagnostics."""

import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import urllib.error


SPEC = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
REPOSITORY = "gridctl/homebrew-tap"
RESOURCE = "contents/Casks/gridctl.rb"


def classify_body(body):
    """Return a fixed category, never an API-provided message or credential."""
    try:
        message = json.loads(body).get("message")
    except (ValueError, AttributeError, UnicodeError):
        return "unrecognized_response"
    return {
        "Bad credentials": "bad_credentials",
        "Requires authentication": "authentication_required",
        "Resource not accessible by personal access token": "token_access_denied",
        "Resource not accessible by integration": "integration_access_denied",
        "Not Found": "not_found",
    }.get(message, "unrecognized_response") if isinstance(message, str) else "unrecognized_response"


def probe_python(token):
    try:
        release.api(REPOSITORY, RESOURCE, method="GET", token=token)
        return {"status": 200, "result": "success"}
    except urllib.error.HTTPError as error:
        try:
            category = classify_body(error.read(8192))
        except Exception:
            category = "unreadable_response"
        finally:
            error.close()
        return {"status": error.code, "result": category}
    except Exception:
        # Exception text can contain headers, tokens, or reflected server input.
        return {"status": None, "result": "request_failed"}


def probe_gh(token):
    with tempfile.TemporaryDirectory(prefix="tap-auth-") as clean:
        # Do not inherit alternate credentials, debug settings, or CLI config.
        env = {key: os.environ[key] for key in ("PATH", "SYSTEMROOT") if key in os.environ}
        env.update(GH_TOKEN=token, HOME=clean, GH_CONFIG_DIR=clean,
                   GH_HOST="github.com", GH_PROMPT_DISABLED="1", NO_COLOR="1")
        try:
            response = subprocess.run([
                "gh", "api", "--hostname", "github.com", "--method", "GET", "--include",
                "-H", "Accept: application/vnd.github+json",
                "-H", "X-GitHub-Api-Version: 2022-11-28",
                f"repos/{REPOSITORY}/{RESOURCE}",
            ], env=env, capture_output=True, text=True, timeout=120)
        except Exception:
            return {"status": None, "result": "request_failed"}
    header, _, body = response.stdout.replace("\r\n", "\n").partition("\n\n")
    match = re.match(r"HTTP/\S+ (\d{3})\b", header)
    status = int(match[1]) if match else None
    return {"status": status, "result": "success" if response.returncode == 0 and status == 200
            else classify_body(body)}


def main():
    token = os.environ.get("GORELEASER_TOKEN", "")
    report = {"repository": REPOSITORY, "resource": RESOURCE, "method": "GET",
              "credential": {"present": bool(token),
                             "surrounding_whitespace": token != token.strip(),
                             "contains_cr_or_lf": "\r" in token or "\n" in token}}
    if token:
        # Pass the stored value unchanged to both clients.
        report["python_helper"] = probe_python(token)
        report["github_cli"] = probe_gh(token)
    print(json.dumps(report, indent=2))
    return 0 if token and all(report[key]["result"] == "success"
                              for key in ("python_helper", "github_cli")) else 1


if __name__ == "__main__":
    raise SystemExit(main())
