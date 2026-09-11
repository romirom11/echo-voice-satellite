"""
Controller unit tests cover the pure-logic modules only (em_eq,
em_scenes, em_oww_models, version) — nothing that needs openwakeword,
aiohttp, a database, or a device. Run from anywhere:

    cd controller && python -m pytest
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))


import pytest


@pytest.fixture
def bundled_stop_model(tmp_path, monkeypatch):
    """
    The maintainer's deployment: the controller image bundles the built-in
    stop classifier. Tests that drive a device through config push and then
    expect `stop_status {model: "stop", ready: true}` to arm the stop word
    need it, because on a machine without /app/models/stopword/stop.onnx the
    optional-stop-word policy (em_oww_assets.effective_stop_model) resolves
    the default "stop" to OFF and pushes "" instead.
    """
    import em_oww_models
    bundled = tmp_path / "stopword" / "stop.onnx"
    bundled.parent.mkdir(parents=True)
    bundled.write_bytes(b"onnx")
    monkeypatch.setattr(em_oww_models, "BUILTIN_STOP_PATH", bundled)
    return bundled
