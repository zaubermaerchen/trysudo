"""Release verification rejects incomplete or unsafe download sets."""

import hashlib
import importlib.util
import io
from pathlib import Path
import tarfile
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location(
    "verify_release", Path(__file__).with_name("verify_release.py")
)


class ReleaseVerificationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.names = [
            f"trysudo-v0.1.0-{os_name}-{arch}.tar.gz"
            for os_name in ("linux", "darwin")
            for arch in ("amd64", "arm64")
        ]
        for name in self.names:
            self.archive(name)
        self.checksums()
        self.module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(self.module)

    def archive(self, name, mode=0o755, members=1, symlink=False):
        with tarfile.open(self.directory / name, "w:gz") as archive:
            for _ in range(members):
                member = tarfile.TarInfo("trysudo")
                member.mode = mode
                member.size = 6
                if symlink:
                    member.type = tarfile.SYMTYPE
                    member.linkname = "/tmp/trysudo"
                    member.size = 0
                archive.addfile(member, io.BytesIO(b"binary"))

    def checksums(self):
        (self.directory / "SHA256SUMS").write_text("".join(
            f"{hashlib.sha256((self.directory / name).read_bytes()).hexdigest()}  {name}\n"
            for name in sorted(self.names)
        ))

    def test_valid_set(self):
        self.module.verify("v0.1.0", self.directory)

    def test_rejects_missing_extra_or_corrupt_files(self):
        for kind in ("missing", "extra", "checksum"):
            with self.subTest(kind=kind):
                if kind == "missing":
                    data = (self.directory / self.names[0]).read_bytes()
                    (self.directory / self.names[0]).unlink()
                elif kind == "extra":
                    (self.directory / "unexpected").touch()
                else:
                    (self.directory / "SHA256SUMS").write_text("0" * 64 + "\n")
                with self.assertRaises(ValueError):
                    self.module.verify("v0.1.0", self.directory)
                if kind == "missing":
                    (self.directory / self.names[0]).write_bytes(data)
                elif kind == "extra":
                    (self.directory / "unexpected").unlink()
                self.checksums()

    def test_rejects_non_executable_duplicate_and_link_members(self):
        for options in ({"mode": 0o644}, {"members": 2}, {"symlink": True}):
            with self.subTest(options=options):
                self.archive(self.names[0], **options)
                self.checksums()
                with self.assertRaises(ValueError):
                    self.module.verify("v0.1.0", self.directory)

    def test_rejects_unsafe_version(self):
        with self.assertRaises(ValueError):
            self.module.verify("../v0.1.0", self.directory)


if __name__ == "__main__":
    unittest.main()
