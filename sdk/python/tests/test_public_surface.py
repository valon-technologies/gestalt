"""The public surface contract: every export resolves and is introspectable."""

import unittest

import gestalt


class PublicSurfaceTest(unittest.TestCase):
    def test_all_entries_resolve(self) -> None:
        for name in gestalt.__all__:
            self.assertTrue(hasattr(gestalt, name), f"__all__ entry {name!r} does not resolve")

    def test_all_has_no_duplicates(self) -> None:
        seen = set()
        for name in gestalt.__all__:
            self.assertNotIn(name, seen, f"duplicate __all__ entry {name!r}")
            seen.add(name)

    def test_dir_covers_all(self) -> None:
        self.assertLessEqual(set(gestalt.__all__), set(dir(gestalt)))


if __name__ == "__main__":
    unittest.main()
