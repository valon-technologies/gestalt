import gestalt

GREETING = "Hello"


class GreetInput(gestalt.Model):
    name: str = gestalt.field(description="Name to greet", default="World")


class GreetOutput(gestalt.Model):
    message: str


def configure(_name: str, config: dict[str, object]) -> None:
    global GREETING
    GREETING = str(config.get("greeting", "Hello"))


@gestalt.operation(
    method="GET",
    description="Return a greeting message",
    read_only=True,
)
def greet(input: GreetInput, _req: gestalt.Request) -> GreetOutput:
    return GreetOutput(message=f"{GREETING}, {input.name}!")
