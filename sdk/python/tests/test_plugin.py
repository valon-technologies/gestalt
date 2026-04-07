import asyncio
import unittest

from gestalt import OK, Model, Plugin, Request


class PluginTests(unittest.TestCase):
    """Plugin authoring behavior tests."""

    def test_async_configure_handler_is_awaited(self) -> None:
        """Async configure handlers should finish before configure_provider returns."""
        plugin = Plugin("example")
        seen: dict[str, object] = {}

        @plugin.configure
        async def configure(name: str, config: dict[str, object]) -> None:
            await asyncio.sleep(0)
            seen["name"] = name
            seen["config"] = config

        plugin.configure_provider("instance-name", {"greeting": "hi"})

        self.assertEqual(
            seen,
            {
                "name": "instance-name",
                "config": {"greeting": "hi"},
            },
        )

    def test_async_operation_handler_is_awaited(self) -> None:
        """Async operation handlers should resolve before Plugin.execute serializes the response."""
        plugin = Plugin("example")

        class GreetingInput(Model):
            name: str

        @plugin.operation(id="greet")
        async def greet(input: GreetingInput, request: Request) -> object:
            await asyncio.sleep(0)
            return OK({"message": f"Hello, {input.name}", "token": request.token})

        status, body = plugin.execute(
            "greet",
            {"name": "Ada"},
            Request(token="tok-123"),
        )

        self.assertEqual(status, 200)
        self.assertEqual(body, '{"message":"Hello, Ada","token":"tok-123"}')


if __name__ == "__main__":
    unittest.main()
