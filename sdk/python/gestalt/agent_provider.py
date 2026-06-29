"""Provider-authoring types for Gestalt agent turn output.

Use these types when implementing :class:`~gestalt.AgentProvider` and returning
turn results from ``create_turn`` / ``get_turn``. They match the wire shape the
SDK serializes on the provider request path.

Client code that calls :class:`~gestalt.Agent` should use the top-level
``gestalt.AgentTurn`` exports instead — those denote the generated client types
returned by ``get_turn`` / ``create_turn``.
"""

from __future__ import annotations

from ._agent import (
    AgentTurn,
    AgentTurnOutput,
    AgentTurnStructuredOutput,
)

__all__ = [
    "AgentTurn",
    "AgentTurnOutput",
    "AgentTurnStructuredOutput",
]
