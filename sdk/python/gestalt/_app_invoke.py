"""Handwritten GraphQL invoke convenience over the generated pieces.

GraphQL invocation deliberately stays out of the json_result annotation
vocabulary: the generated ``App.invoke_graphql`` returns the raw operation
result, and the generated invoke support exposes the envelope-plus-errors
decoding. This helper keeps the one ergonomic step the deleted facade used to
provide.
"""

from __future__ import annotations

from typing import Any

from .app import App
from .invoke_support import InvokeError, decode_graphql_result
from .rpc_support import JsonValue


def invoke_graphql(
    client: App,
    app: str,
    document: str,
    *,
    connection: str = "",
    instance: str = "",
    idempotency_key: str = "",
    variables: dict[str, JsonValue] | None = None,
) -> Any:
    """Invoke the GraphQL surface of another app and decode the JSON result.

    Invokes through the generated :class:`~gestalt.app.App` client's
    ``invoke_graphql`` method and decodes like
    :func:`~gestalt.invoke_support.decode_graphql_result`, raising
    :class:`~gestalt.invoke_support.InvokeError` when the response carries a
    GraphQL ``errors`` array.
    """
    trimmed_document = document.strip()
    if not trimmed_document:
        raise InvokeError("graphql document is required", app=app, operation="graphql")
    result = client.invoke_graphql(
        app=app,
        document=trimmed_document,
        connection=connection,
        instance=instance,
        idempotency_key=idempotency_key.strip(),
        variables=variables if variables is not None else {},
    )
    return decode_graphql_result(app, result.status, result.body)
