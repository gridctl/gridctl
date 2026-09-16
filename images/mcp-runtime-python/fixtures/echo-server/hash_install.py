from pathlib import Path
import hashlib
import subprocess
import sys

wheels = list(Path("/tmp/wheels").glob("*.whl"))
if len(wheels) != 1:
    raise SystemExit(f"expected one wheel, found {wheels!r}")
digest = hashlib.sha256(wheels[0].read_bytes()).hexdigest()
requirements = Path("/tmp/requirements.txt")
requirements.write_text(f"{wheels[0].as_posix()} --hash=sha256:{digest}\n")
subprocess.check_call([
    sys.argv[1], "pip", "install",
    "--python", "/app/.venv/bin/python",
    "--require-hashes",
    "-r", str(requirements),
])
