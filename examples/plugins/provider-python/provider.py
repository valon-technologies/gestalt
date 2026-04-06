from __future__ import annotations

import gestalt

plugin = gestalt.Plugin.from_manifest("plugin.yaml")


class GreetInput(gestalt.Model):
    name: str = gestalt.field(description="Name to greet", default="World")


class GreetOutput(gestalt.Model):
    message: str


@plugin.operation(
    id="greet",
    method="GET",
    description="Return a greeting message",
    read_only=True,
)
def greet(input: GreetInput, _req: gestalt.Request) -> GreetOutput:
    return GreetOutput(message=f"Hello, {input.name}!")


if __name__ == "__main__":
    plugin.serve()
