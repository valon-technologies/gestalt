Python API Reference
====================

These pages document the authored Python API that provider authors use to
build Gestalt integrations, authentication providers, caches, and S3
backends. The supported import surface is the top-level :mod:`gestalt`
package:

.. code-block:: python

   from gestalt import Model, App, Cache, IndexedDB, S3

Everything the package exports is documented below; the root :mod:`gestalt`
page covers the flat import surface, and each public submodule page covers
its service's native types and client. Transport serialization details are
intentionally private.

.. autosummary::
   :toctree: api
   :recursive:

   gestalt
