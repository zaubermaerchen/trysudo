"""Verify the complete archive/checksum set before release publication."""

import hashlib
from pathlib import Path
import re
import sys
import tarfile


def verify(version, directory):
    if not re.fullmatch(r"[A-Za-z0-9._+-]+", version):
        raise ValueError("invalid version label")
    names = sorted(
        f"trysudo-{version}-{os_name}-{arch}.tar.gz"
        for os_name in ("linux", "darwin")
        for arch in ("amd64", "arm64")
    )
    if {path.name for path in directory.iterdir()} != set(names + ["SHA256SUMS"]):
        raise ValueError("release must contain exactly four archives and SHA256SUMS")
    for name in names + ["SHA256SUMS"]:
        if not (directory / name).is_file() or (directory / name).is_symlink():
            raise ValueError(f"release asset must be a regular file: {name}")
    expected = "".join(
        f"{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n"
        for name in names
    )
    if (directory / "SHA256SUMS").read_text() != expected:
        raise ValueError("SHA256SUMS does not match the complete release set")
    for name in names:
        with tarfile.open(directory / name, "r:gz") as archive:
            members = archive.getmembers()
            if (len(members) != 1 or members[0].name != "trysudo"
                    or not members[0].isfile() or members[0].mode != 0o755
                    or members[0].size == 0):
                raise ValueError(f"archive must contain only executable trysudo: {name}")


if __name__ == "__main__":
    verify(sys.argv[1], Path(sys.argv[2]))
