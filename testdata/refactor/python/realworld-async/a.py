async def fetch_user(client, user_id):
    response = await client.get(f"/users/{user_id}")
    response.raise_for_status()
    payload = response.json()
    return payload
