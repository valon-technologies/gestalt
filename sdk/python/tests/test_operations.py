import contextlib
import io
import json
import unittest
from dataclasses import dataclass

from gestalt import Request, Response
from gestalt._operations import (
    OperationDefinition,
    OperationResult,
    decode_input,
    execute_operation,
    inspect_handler,
    run_sync,
)


class InspectHandlerTests(unittest.TestCase):
    """Tests for inspect_handler, which extracts input type and Request flag from handler signatures."""

    def test_no_parameters(self) -> None:
        """A handler with no parameters has no input type and no Request."""
        def handler() -> str:
            return "ok"

        input_type, takes_request = inspect_handler(handler)
        self.assertIsNone(input_type)
        self.assertFalse(takes_request)

    def test_single_typed_parameter(self) -> None:
        """A handler with one typed parameter uses that as its input type."""
        def handler(name: str) -> str:
            return name

        input_type, takes_request = inspect_handler(handler)
        self.assertIs(input_type, str)
        self.assertFalse(takes_request)

    def test_single_request_parameter(self) -> None:
        """A handler with only a Request parameter takes request but no input."""
        def handler(req: Request) -> str:
            return req.token

        input_type, takes_request = inspect_handler(handler)
        self.assertIsNone(input_type)
        self.assertTrue(takes_request)

    def test_input_and_request(self) -> None:
        """A handler with an input type and a Request parameter."""
        def handler(name: str, req: Request) -> str:
            return name

        input_type, takes_request = inspect_handler(handler)
        self.assertIs(input_type, str)
        self.assertTrue(takes_request)

    def test_too_many_parameters_raises(self) -> None:
        """A handler with more than 2 parameters should raise TypeError."""
        def handler(a: str, _b: Request, _c: int) -> str:
            return a

        with self.assertRaises(TypeError):
            inspect_handler(handler)

    def test_second_parameter_must_be_request(self) -> None:
        """If there are two parameters, the second must be Request."""
        def handler(a: str, _b: int) -> str:
            return a

        with self.assertRaises(TypeError):
            inspect_handler(handler)


class DecodeInputTests(unittest.TestCase):
    """Tests for decode_input, which converts raw params dicts into typed values."""

    def test_decode_string(self) -> None:
        """String input decoded from a single-field dict."""
        result = decode_input(str, {"value": "hello"})
        self.assertEqual(result, "hello")

    def test_decode_int(self) -> None:
        """Int input decoded from a single-field dict."""
        result = decode_input(int, {"value": 42})
        self.assertEqual(result, 42)

    def test_decode_int_from_string(self) -> None:
        """Int input parsed from a string value."""
        result = decode_input(int, {"value": "42"})
        self.assertEqual(result, 42)

    def test_decode_int_rejects_float(self) -> None:
        """Float values should not silently become ints."""
        with self.assertRaises(TypeError):
            decode_input(int, {"value": 3.5})

    def test_decode_bool_true(self) -> None:
        """Boolean true from native bool and string."""
        self.assertTrue(decode_input(bool, {"v": True}))
        self.assertTrue(decode_input(bool, {"v": "true"}))
        self.assertTrue(decode_input(bool, {"v": "TRUE"}))
        self.assertTrue(decode_input(bool, {"v": "1"}))

    def test_decode_bool_false(self) -> None:
        """Boolean false from native bool and string."""
        self.assertFalse(decode_input(bool, {"v": False}))
        self.assertFalse(decode_input(bool, {"v": "false"}))
        self.assertFalse(decode_input(bool, {"v": "0"}))

    def test_decode_bool_rejects_unexpected_strings(self) -> None:
        """Unexpected string values should raise TypeError."""
        with self.assertRaises(TypeError):
            decode_input(bool, {"v": "yes"})
        with self.assertRaises(TypeError):
            decode_input(bool, {"v": "on"})

    def test_decode_float(self) -> None:
        """Float input from int and float values."""
        self.assertEqual(decode_input(float, {"v": 3.14}), 3.14)
        self.assertEqual(decode_input(float, {"v": 3}), 3.0)

    def test_decode_dataclass(self) -> None:
        """Dataclass input decoded from a dict."""
        @dataclass
        class Point:
            x: int
            y: int

        result = decode_input(Point, {"x": 1, "y": 2})
        self.assertEqual(result, Point(x=1, y=2))

    def test_decode_optional(self) -> None:
        """Optional types decode None correctly."""
        result = decode_input(str | None, {"v": None})
        self.assertIsNone(result)

    def test_decode_list(self) -> None:
        """List input decoded from a dict containing a list."""
        @dataclass
        class Input:
            items: list[int]

        result = decode_input(Input, {"items": [1, 2, 3]})
        self.assertEqual(result.items, [1, 2, 3])

    def test_primitive_rejects_multi_field_dict(self) -> None:
        """Primitive types reject dicts with multiple fields."""
        with self.assertRaises(TypeError):
            decode_input(str, {"a": "hello", "b": "world"})


class ExecuteOperationTests(unittest.TestCase):
    """Tests for execute_operation, the top-level operation dispatch function."""

    def test_successful_execution(self) -> None:
        """A successful handler returns status 200 and the JSON body."""
        def handler() -> dict[str, str]:
            return {"status": "ok"}

        operation = OperationDefinition(
            id="test",
            method="GET",
            title="",
            description="",
            tags=[],
            read_only=False,
            visible=None,
            handler=handler,
            input_type=None,
            takes_request=False,
        )

        result = execute_operation(operation, params={}, request=Request())

        self.assertIsInstance(result, OperationResult)
        self.assertEqual(result.status, 200)
        self.assertEqual(json.loads(result.body), {"status": "ok"})

    def test_missing_operation(self) -> None:
        """A None operation returns 404."""
        result = execute_operation(None, params={}, request=Request())

        self.assertEqual(result.status, 404)
        self.assertIn("unknown operation", json.loads(result.body)["error"])

    def test_handler_exception_returns_500(self) -> None:
        """An exception in the handler returns 500 with the error message."""
        def handler() -> None:
            raise RuntimeError("boom")

        operation = OperationDefinition(
            id="test",
            method="GET",
            title="",
            description="",
            tags=[],
            read_only=False,
            visible=None,
            handler=handler,
            input_type=None,
            takes_request=False,
        )

        stderr_buffer = io.StringIO()
        with contextlib.redirect_stderr(stderr_buffer):
            result = execute_operation(operation, params={}, request=Request())

        self.assertEqual(result.status, 500)
        self.assertIn("boom", json.loads(result.body)["error"])

    def test_response_wrapper_preserves_status(self) -> None:
        """A handler returning a Response should preserve its status code."""
        def handler() -> Response[str]:
            return Response(status=201, body="created")

        operation = OperationDefinition(
            id="test",
            method="POST",
            title="",
            description="",
            tags=[],
            read_only=False,
            visible=None,
            handler=handler,
            input_type=None,
            takes_request=False,
        )

        result = execute_operation(operation, params={}, request=Request())
        self.assertEqual(result.status, 201)

    def test_handler_with_input_and_request(self) -> None:
        """A handler that takes both input and Request receives both."""
        @dataclass
        class Input:
            name: str

        def handler(input: Input, req: Request) -> dict[str, str]:
            return {"name": input.name, "token": req.token}

        input_type, takes_request = inspect_handler(handler)
        operation = OperationDefinition(
            id="test",
            method="POST",
            title="",
            description="",
            tags=[],
            read_only=False,
            visible=None,
            handler=handler,
            input_type=input_type,
            takes_request=takes_request,
        )

        result = execute_operation(
            operation,
            params={"name": "alice"},
            request=Request(token="tok-123"),
        )
        self.assertEqual(result.status, 200)
        body = json.loads(result.body)
        self.assertEqual(body["name"], "alice")
        self.assertEqual(body["token"], "tok-123")


class RunSyncTests(unittest.TestCase):
    """Tests for run_sync, which bridges async handlers to sync execution."""

    def test_sync_value_passthrough(self) -> None:
        """Non-awaitable values are returned as-is."""
        self.assertEqual(run_sync(42), 42)
        self.assertEqual(run_sync("hello"), "hello")

    def test_async_value_executed(self) -> None:
        """Awaitable values are executed via asyncio.run."""
        async def coro() -> int:
            return 42

        self.assertEqual(run_sync(coro()), 42)


if __name__ == "__main__":
    unittest.main()
