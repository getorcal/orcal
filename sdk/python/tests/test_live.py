import uuid


def test_readme_example_runs_end_to_end(live, image):
    with live.sandbox(image=image) as sb:
        sb.files.write("/app/marker.txt", "original")
        result = sb.exec("cat /app/marker.txt")
        assert result.exit_code == 0
        assert result.stdout.strip() == "original"

        stream = sb.exec("echo streamed", stream=True)
        chunks = [frame.data for frame in stream]
        assert b"streamed" in b"".join(chunks)
        assert stream.exit_code == 0

        snap = sb.snapshot(name=f"v-{uuid.uuid4().hex[:8]}")

    try:
        with snap.fork() as branch:
            forked = branch.exec("cat /app/marker.txt")
            assert forked.exit_code == 0, "a fork must carry the parent snapshot's filesystem"
            assert forked.stdout.strip() == "original"
    finally:
        snap.delete()


def test_files_round_trip_binary(live, image):
    payload = bytes(range(256))
    with live.sandbox(image=image) as sb:
        sb.files.write("/app/blob.bin", payload)
        assert sb.files.read("/app/blob.bin") == payload


def test_exit_code_and_stderr_are_reported(live, image):
    with live.sandbox(image=image) as sb:
        result = sb.exec("echo oops >&2; exit 7")
        assert result.exit_code == 7
        assert "oops" in result.stderr


def test_a_none_network_sandbox_reports_its_mode(live, image):
    with live.sandbox(image=image, network="none") as sb:
        assert sb.network == "none"


def test_sandbox_is_destroyed_when_the_body_raises(live, image):
    sandbox_id = None
    try:
        with live.sandbox(image=image) as sb:
            sandbox_id = sb.id
            raise RuntimeError("boom")
    except RuntimeError:
        pass
    assert sandbox_id is not None
    assert live.get_sandbox(sandbox_id).state == "destroyed"


def test_listing_sandboxes_paginates(live, image):
    with live.sandbox(image=image), live.sandbox(image=image):
        ids = [sb.id for sb in live.sandboxes()]
        assert len(ids) == len(set(ids))
