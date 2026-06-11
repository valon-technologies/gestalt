"""Tests for the handwritten GraphQL invoke convenience."""

from __future__ import annotations

import json
import unittest
from typing import Any, cast

from gestalt import InvokeError, invoke_graphql
from gestalt.app import App, OperationResult


class FakeApp:
    def __init__(self, body: dict[str, Any], status: int = 200) -> None:
        self.calls: list[dict[str, Any]] = []
        self._result = OperationResult(
            status=status, body=json.dumps(body).encode(), headers={}
        )

    def invoke_graphql(self, **kwargs: Any) -> OperationResult:
        self.calls.append(kwargs)
        return self._result


class InvokeGraphQLTest(unittest.TestCase):
    def test_validates_document_and_decodes(self) -> None:
        app = FakeApp({"data": {"viewer": {"id": "user-1"}}, "errors": []})
        decoded = invoke_graphql(
            cast(App, app),
            "linear",
            " query { viewer { id } } ",
            variables={"first": 1},
            idempotency_key=" gq-1 ",
        )
        self.assertEqual(decoded, {"data": {"viewer": {"id": "user-1"}}, "errors": []})
        self.assertEqual(
            app.calls,
            [
                {
                    "app": "linear",
                    "document": "query { viewer { id } }",
                    "connection": "",
                    "instance": "",
                    "idempotency_key": "gq-1",
                    "variables": {"first": 1},
                }
            ],
        )

        with self.assertRaises(InvokeError):
            invoke_graphql(cast(App, app), "linear", "   ")

    def test_raises_on_graphql_errors(self) -> None:
        app = FakeApp({"data": None, "errors": [{"message": "boom"}]})
        with self.assertRaises(InvokeError) as raised:
            invoke_graphql(cast(App, app), "linear", "query { x }")
        self.assertEqual(raised.exception.code, "graphql_errors")


if __name__ == "__main__":
    unittest.main()
