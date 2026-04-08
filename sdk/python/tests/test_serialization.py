import json
import pathlib
import unittest
from dataclasses import dataclass

from gestalt._serialization import json_body


class JsonBodyTests(unittest.TestCase):

    def test_json_body_structures(self) -> None:
        @dataclass
        class Inner:
            value: int

        @dataclass
        class Outer:
            inner: Inner

        self.assertEqual(json.loads(json_body({"key": "value"})), {"key": "value"})
        self.assertEqual(json.loads(json_body({"outer": {"inner": 1}})), {"outer": {"inner": 1}})
        self.assertEqual(json.loads(json_body([1, 2, 3])), [1, 2, 3])
        self.assertEqual(json.loads(json_body(Inner(value=42))), {"value": 42})
        self.assertEqual(json.loads(json_body(Outer(inner=Inner(value=42)))), {"inner": {"value": 42}})

    def test_json_body_special_types(self) -> None:
        self.assertEqual(json.loads(json_body({"path": pathlib.Path("/tmp/file.txt")})), {"path": "/tmp/file.txt"})
        self.assertEqual(json.loads(json_body({1})), [1])
        self.assertEqual(json.loads(json_body((1, 2))), [1, 2])

    def test_json_body_format(self) -> None:
        self.assertEqual(json_body({"a": 1}), '{"a":1}')
        self.assertEqual(json_body(42), "42")
        self.assertEqual(json_body("hello"), '"hello"')
        self.assertEqual(json_body(True), "true")
        self.assertEqual(json_body(None), "null")


if __name__ == "__main__":
    unittest.main()
