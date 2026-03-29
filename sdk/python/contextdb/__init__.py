"""contextdb Python SDK — connect to a contextdb server via REST."""

from contextdb.client import ContextDB, Namespace
from contextdb.async_client import AsyncContextDB, AsyncNamespace

__all__ = ["ContextDB", "Namespace", "AsyncContextDB", "AsyncNamespace"]
__version__ = "0.2.0"
