async def fetch_order(client, order_id):
    response = await client.get(f"/orders/{order_id}")
    response.raise_for_status()
    payload = response.json()
    return payload
