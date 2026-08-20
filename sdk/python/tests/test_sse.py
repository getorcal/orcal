from orcal._sse import SSEParser


def drain(parser, *chunks):
    events = []
    for chunk in chunks:
        events.extend(parser.feed(chunk))
    return events


def test_parses_a_whole_event():
    events = drain(SSEParser(), b'event: output\ndata: {"offset":10,"stream":"stdout","data":"aGk="}\n\n')
    assert events == [("output", {"offset": 10, "stream": "stdout", "data": "aGk="})]


def test_parses_an_event_split_across_two_reads():
    parser = SSEParser()
    first = parser.feed(b'event: output\ndata: {"offset":1,"stre')
    second = parser.feed(b'am":"stdout","data":"aGk="}\n\n')
    assert first == []
    assert second == [("output", {"offset": 1, "stream": "stdout", "data": "aGk="})]


def test_parses_two_events_in_one_read():
    events = drain(
        SSEParser(),
        b'event: gap\ndata: {"offset":5}\n\nevent: exit\ndata: {"state":"exited","exit_code":0,"truncated":false}\n\n',
    )
    assert [name for name, _ in events] == ["gap", "exit"]
    assert events[1][1]["exit_code"] == 0


def test_ignores_comment_keepalives():
    events = drain(SSEParser(), b': keepalive\n\nevent: gap\ndata: {"offset":2}\n\n')
    assert [name for name, _ in events] == ["gap"]


def test_incomplete_trailing_event_is_not_emitted():
    assert drain(SSEParser(), b'event: output\ndata: {"offset":1}') == []


def test_malformed_json_frame_is_silently_skipped():
    events = drain(
        SSEParser(),
        b'event: output\ndata: {invalid json}\n\nevent: gap\ndata: {"offset":7}\n\n',
    )
    assert [name for name, _ in events] == ["gap"]
    assert events[0][1]["offset"] == 7


def test_multiple_complete_frames_plus_trailing_partial_in_one_feed():
    parser = SSEParser()
    first_feed = parser.feed(
        b'event: output\ndata: {"offset":1,"stream":"stdout","data":"dGVzdA=="}\n\n'
        b'event: gap\ndata: {"offset":2}\n\n'
        b'event: exit\ndata: {"state":"ex'
    )
    assert len(first_feed) == 2
    assert [name for name, _ in first_feed] == ["output", "gap"]

    second_feed = parser.feed(b'ited","exit_code":42,"truncated":true}\n\n')
    assert len(second_feed) == 1
    assert second_feed[0][0] == "exit"
    assert second_feed[0][1]["exit_code"] == 42
