from pathlib import Path


def test_stop_upload_route_and_provisioning_include_stop_model():
    src = (Path(__file__).resolve().parents[1] / "em_api.py").read_text()
    assert '"/api/oww_models/stop/upload"' in src
    manifest = src[src.index("async def _get_provision_oww_manifest"):]
    # The stop model reaches the manifest through the shared helper, which
    # omits it when the stop word resolves to off (optional stop word).
    assert "_fleet_wanted_models(fleet)" in manifest
    helper = src[src.index("def _fleet_wanted_models"):src.index("async def _get_provision_oww_manifest")]
    assert 'fleet.get("stopModel")' in helper
    assert "effective_stop_model" in helper
    assert "_post_stop_model_upload" in src


def test_stop_upload_is_atomic_and_selects_fleet_model():
    src = (Path(__file__).resolve().parents[1] / "em_api.py").read_text()
    body = src[src.index("async def _post_stop_model_upload"):]
    assert 'destination = directory / "stop.onnx"' in body
    assert "os.replace(tmp_path, destination)" in body
    assert 'config["stopModel"]' in body
