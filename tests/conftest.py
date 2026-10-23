"""Shared fixtures: load `opsforge` (no .py suffix) as a module and the repo's topology."""

import copy
import importlib.machinery
import importlib.util
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "lab"))


def _load_cli():
    loader = importlib.machinery.SourceFileLoader("opsforge_cli", str(ROOT / "opsforge"))
    spec = importlib.util.spec_from_loader("opsforge_cli", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


cli = _load_cli()


@pytest.fixture
def topo():
    """A fresh copy of the repo's topology.yml that tests may mutate."""
    return copy.deepcopy(cli.load_topology())


@pytest.fixture
def multi():
    """A fresh copy of the multi-cluster layout (topologies/multi-cluster.yml)."""
    return copy.deepcopy(cli.load_topology(ROOT / "topologies" / "multi-cluster.yml"))
