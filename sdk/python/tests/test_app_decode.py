"""Conformance tests for the generated JSON operation-envelope decode,
driven by the shared fixtures in sdk/testdata/app_invoke. The fixture suite is
the normative spec of the envelope semantics across all four SDK languages."""

from __future__ import annotations

import pathlib
import unittest
from typing import Any

from gestalt import Response
from gestalt.invoke_support import (
    InvokeError,
    decode_app_result,
    decode_graphql_result,
    ok,
    raise_for_status,
)

FIXTURE_ROOT = pathlib.Path(__file__).resolve().parents[2] / "testdata" / "app_invoke"


def fixture(name: str) -> bytes:
    return (FIXTURE_ROOT / name).read_bytes()


class AppDecodeTests(unittest.TestCase):
    def decode(self, name: str, status: int = 200) -> Any:
        return decode_app_result("github", "get_issue", status, fixture(name))

    def test_app_decode_fixtures(self) -> None:
        self.assertEqual(self.decode("success_envelope.json"), {"id": 1})
        self.assertEqual(
            self.decode("plain_ok.json"),
            {"pull_request": {"id": 123, "title": "Fix transport"}},
        )
        self.assertEqual(self.decode("empty_body.json"), {})
        self.assertEqual(
            self.decode("success_missing_data.json"),
            {"status": "success", "ok": True},
        )
        self.assertIsNone(self.decode("success_null_data.json"))
        self.assertEqual(
            self.decode("unknown_status.json"),
            {"status": "pending", "data": {"id": 2}},
        )
        self.assertEqual(
            self.decode("non_string_status.json"),
            {"status": True, "data": {"id": 3}},
        )
        self.assertEqual(self.decode("array_ok.json"), [1, 2, 3])
        self.assertEqual(self.decode("primitive_ok.json"), "ok")

    def test_app_decode_errors(self) -> None:
        with self.assertRaises(InvokeError) as envelope:
            self.decode("error_envelope.json")
        self.assertEqual(envelope.exception.app, "github")
        self.assertEqual(envelope.exception.operation, "get_issue")
        self.assertIsNone(envelope.exception.status)
        self.assertEqual(envelope.exception.code, "missing_credential")
        self.assertEqual(str(envelope.exception), "missing credential")

        with self.assertRaises(InvokeError) as http_error:
            self.decode("http_401.json", 401)
        self.assertEqual(http_error.exception.status, 401)
        self.assertEqual(http_error.exception.code, "unauthorized")
        self.assertEqual(str(http_error.exception), "unauthorized")
        self.assertTrue(http_error.exception.raw_body)

        with self.assertRaises(InvokeError) as redirect:
            self.decode("http_302.json", 302)
        self.assertEqual(redirect.exception.status, 302)

        with self.assertRaises(InvokeError) as invalid:
            self.decode("invalid_json.txt")
        self.assertEqual(str(invalid.exception), "app invoke response is not valid JSON")

    def test_ok_boundaries(self) -> None:
        self.assertFalse(ok(199))
        self.assertTrue(ok(200))
        self.assertTrue(ok(204))
        self.assertTrue(ok(299))
        self.assertFalse(ok(300))
        self.assertFalse(ok(401))

    def test_raise_for_status(self) -> None:
        self.assertIsNone(
            raise_for_status("github", "get_issue", 200, fixture("success_envelope.json"))
        )
        with self.assertRaises(InvokeError) as redirect:
            raise_for_status("github", "get_issue", 302, fixture("http_302.json"))
        self.assertEqual(redirect.exception.status, 302)

        with self.assertRaises(InvokeError) as http_error:
            raise_for_status("github", "get_issue", 401, fixture("http_401.json"))
        self.assertEqual(http_error.exception.app, "github")
        self.assertEqual(http_error.exception.operation, "get_issue")
        self.assertEqual(http_error.exception.status, 401)
        self.assertEqual(http_error.exception.code, "unauthorized")
        self.assertEqual(str(http_error.exception), "unauthorized")
        self.assertTrue(http_error.exception.raw_body)

        with self.assertRaises(InvokeError) as opaque:
            raise_for_status("github", "get_issue", 503, fixture("invalid_json.txt"))
        self.assertEqual(opaque.exception.status, 503)
        self.assertIsNone(opaque.exception.code)
        self.assertEqual(str(opaque.exception), "app invoke failed with status 503")

    def test_graphql_errors(self) -> None:
        self.assertEqual(
            decode_graphql_result("linear", 200, fixture("graphql_ok.json")),
            {"data": {"viewer": {"id": "user-1"}}, "errors": []},
        )
        self.assertEqual(
            decode_graphql_result("linear", 200, fixture("graphql_malformed_errors.json")),
            {"data": {"viewer": None}, "errors": {"message": "not an array"}},
        )
        with self.assertRaises(InvokeError) as error:
            decode_graphql_result("linear", 200, fixture("graphql_errors.json"))
        self.assertEqual(error.exception.code, "graphql_errors")
        self.assertEqual(error.exception.operation, "graphql")
        self.assertEqual(str(error.exception), "permission denied")
        with self.assertRaises(InvokeError) as envelope_error:
            decode_graphql_result(
                "linear", 200, fixture("graphql_success_envelope_errors.json")
            )
        self.assertEqual(envelope_error.exception.code, "graphql_errors")

    def test_top_level_reexports(self) -> None:
        import gestalt

        self.assertIs(gestalt.InvokeError, InvokeError)
        self.assertIs(gestalt.decode_app_result, decode_app_result)
        self.assertIs(gestalt.decode_graphql_result, decode_graphql_result)
        self.assertIs(gestalt.ok, ok)
        self.assertIs(gestalt.raise_for_status, raise_for_status)

    def test_response_helpers(self) -> None:
        raw = Response(status=200, headers={}, body=fixture("success_envelope.json"))
        self.assertTrue(raw.ok)
        self.assertEqual(raw.bytes(), fixture("success_envelope.json"))
        self.assertEqual(raw.text(), fixture("success_envelope.json").decode("utf-8"))
        self.assertEqual(raw.decode_json(), {"status": "success", "data": {"id": 1}})
        self.assertIs(raw.raise_for_status(), raw)

        failed = Response(status=503, headers={}, body=b"not json")
        self.assertFalse(failed.ok)
        with self.assertRaises(InvokeError):
            failed.raise_for_status()


if __name__ == "__main__":
    unittest.main()
