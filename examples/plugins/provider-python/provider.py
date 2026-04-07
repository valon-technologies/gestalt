import gestalt


class GreetInput(gestalt.Model):
    name: str = gestalt.field(description="Name to greet", default="World")


class GreetOutput(gestalt.Model):
    message: str


@gestalt.operation(
    method="GET",
    description="Return a greeting message",
    read_only=True,
)
def greet(input: GreetInput, _req: gestalt.Request) -> GreetOutput:
    return GreetOutput(message=f"Hello, {input.name}!")
