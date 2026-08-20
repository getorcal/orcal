def paginate(transport, path, params, build):
    query = dict(params or {})
    while True:
        body = transport.request("GET", path, params=query).json()
        items = body.get("items") or []
        for item in items:
            yield build(item)
        cursor = body.get("next_cursor")
        if not cursor:
            return
        query["cursor"] = cursor
