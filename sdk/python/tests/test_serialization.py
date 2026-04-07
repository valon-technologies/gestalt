import json
import pathlib
import unittest
from dataclasses import dataclass

from gestalt._serialization import json_body


class JsonBodyTests(unittest.TestCase):
    """Tests for json_body, which serializes values to compact JSON."""

    def test_dict(self) -> None:
        """Plain dicts should serialize to JSON."""
        result = json.loads(json_body({"key": "value"}))
        self.assertEqual(result, {"key": "value"})

    def test_nested_dict(self) -> None:
        """Nested dicts should serialize recursively."""
        result = json.loads(json_body({"outer": {"inner": 1}}))
        self.assertEqual(result, {"outer": {"inner": 1}})

    def test_list(self) -> None:
        """Lists should serialize to JSON arrays."""
        result = json.loads(json_body([1, 2, 3]))
        self.assertEqual(result, [1, 2, 3])

    def test_dataclass(self) -> None:
        """Dataclass instances should serialize to JSON objects."""
        @dataclass
        class Point:
            x: int
            y: int

        result = json.loads(json_body(Point(x=1, y=2)))
        self.assertEqual(result, {"x": 1, "y": 2})

    def test_nested_dataclass(self) -> None:
        """Nested dataclasses should serialize recursively."""
        @dataclass
        class Inner:
            value: int

        @dataclass
        class Outer:
            inner: Inner

        result = json.loads(json_body(Outer(inner=Inner(value=42))))
        self.assertEqual(result, {"inner": {"value": 42}})

    def test_pathlib_path(self) -> None:
        """Path objects should serialize to their string representation."""
        result = json.loads(json_body({"path": pathlib.Path("/tmp/file.txt")}))
        self.assertEqual(result, {"path": "/tmp/file.txt"})

    def test_set(self) -> None:
        """Sets should serialize to JSON arrays."""
        result = json.loads(json_body({1}))
        self.assertEqual(result, [1])

    def test_tuple(self) -> None:
        """Tuples should serialize to JSON arrays."""
        result = json.loads(json_body((1, 2)))
        self.assertEqual(result, [1, 2])

    def test_compact_format(self) -> None:
        """Output should use compact separators (no spaces)."""
        result = json_body({"a": 1})
        self.assertEqual(result, '{"a":1}')

    def test_primitives(self) -> None:
        """Primitive values should serialize directly."""
        self.assertEqual(json_body(42), "42")
        self.assertEqual(json_body("hello"), '"hello"')
        self.assertEqual(json_body(True), "true")
        self.assertEqual(json_body(None), "null")


if __name__ == "__main__":
    unittest.main()
