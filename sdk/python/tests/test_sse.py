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
