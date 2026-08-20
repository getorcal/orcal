def paginate(transport, path, params, build):
    query = dict(params or {})
    sent_cursor = query.get("cursor")
    while True:
        body = transport.request("GET", path, params=query).json()
        items = body.get("items") or []
        for item in items:
            yield build(item)
        cursor = body.get("next_cursor")
        if not cursor or cursor == sent_cursor:
            return
        sent_cursor = cursor
        query["cursor"] = cursor
