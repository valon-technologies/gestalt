
class ByteStream(Iterator[bytes]):
    """Byte payload stream of a framed read.

    ``read`` buffers the remaining stream — mirroring boto3's
    ``StreamingBody.read`` — and consumes it, so each stream supports one
    buffering call.
    """

    def __init__(self, chunks: Iterator[bytes]) -> None:
        self._chunks = chunks

    def __iter__(self) -> Iterator[bytes]:
        return self

    def __next__(self) -> bytes:
        return next(self._chunks)

    def read(self) -> bytes:
        """Buffer the remaining payload chunks into one bytes value."""
        return b"".join(self._chunks)
