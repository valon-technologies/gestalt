import unittest
from dataclasses import dataclass

from gestalt import Request
from gestalt._operations import (
    decode_input,
    inspect_handler,
)


class InspectHandlerTests(unittest.TestCase):

    def test_no_parameters(self) -> None:
        def handler() -> str:
            return "ok"

        input_type, takes_request = inspect_handler(handler)
        self.assertIsNone(input_type)
        self.assertFalse(takes_request)

    def test_single_typed_parameter(self) -> None:
        def handler(name: str) -> str:
            return name

        input_type, takes_request = inspect_handler(handler)
        self.assertIs(input_type, str)
        self.assertFalse(takes_request)

    def test_single_request_parameter(self) -> None:
        def handler(req: Request) -> str:
            return req.token

        input_type, takes_request = inspect_handler(handler)
        self.assertIsNone(input_type)
        self.assertTrue(takes_request)

    def test_input_and_request(self) -> None:
        def handler(name: str, req: Request) -> str:
            return name

        input_type, takes_request = inspect_handler(handler)
        self.assertIs(input_type, str)
        self.assertTrue(takes_request)

    def test_too_many_parameters_raises(self) -> None:
        def handler(a: str, _b: Request, _c: int) -> str:
            return a

        with self.assertRaises(TypeError):
            inspect_handler(handler)

    def test_second_parameter_must_be_request(self) -> None:
        def handler(a: str, _b: int) -> str:
            return a

        with self.assertRaises(TypeError):
            inspect_handler(handler)


class DecodeInputTests(unittest.TestCase):

    def test_decode_primitives(self) -> None:
        self.assertEqual(decode_input(str, {"value": "hello"}), "hello")
        self.assertEqual(decode_input(int, {"value": 42}), 42)
        self.assertEqual(decode_input(int, {"value": "42"}), 42)
        self.assertEqual(decode_input(float, {"v": 3.14}), 3.14)
        self.assertEqual(decode_input(float, {"v": 3}), 3.0)
        with self.assertRaises(TypeError):
            decode_input(int, {"value": 3.5})

    def test_decode_bool(self) -> None:
        self.assertTrue(decode_input(bool, {"v": True}))
        self.assertTrue(decode_input(bool, {"v": "true"}))
        self.assertTrue(decode_input(bool, {"v": "1"}))
        self.assertFalse(decode_input(bool, {"v": False}))
        self.assertFalse(decode_input(bool, {"v": "false"}))
        self.assertFalse(decode_input(bool, {"v": "0"}))
        with self.assertRaises(TypeError):
            decode_input(bool, {"v": "yes"})
        with self.assertRaises(TypeError):
            decode_input(bool, {"v": "on"})

    def test_decode_structured_types(self) -> None:
        @dataclass
        class Point:
            x: int
            y: int

        @dataclass
        class Container:
            items: list[int]

        self.assertEqual(decode_input(Point, {"x": 1, "y": 2}), Point(x=1, y=2))
        self.assertIsNone(decode_input(str | None, {"v": None}))
        self.assertEqual(decode_input(Container, {"items": [1, 2, 3]}).items, [1, 2, 3])

    def test_decode_edge_cases(self) -> None:
        with self.assertRaises(TypeError):
            decode_input(str, {"a": "hello", "b": "world"})


if __name__ == "__main__":
    unittest.main()
