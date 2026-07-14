defmodule InventoryLedger do
  def add_item(items, capacity, sku, count) do
    if count <= 0 do
      raise ArgumentError, "count must be positive"
    end

    current = Map.get(items, sku, 0)

    if current + count > capacity do
      raise ArgumentError, "capacity exceeded for " <> sku
    end

    Map.put(items, sku, current + count)
  end

  def remove_item(items, sku, count) do
    current = Map.get(items, sku, 0)

    if count > current do
      raise ArgumentError, "cannot remove more than stored"
    end

    Map.put(items, sku, current - count)
  end

  def total_units(items) do
    Enum.reduce(items, 0, fn {_sku, count}, acc -> acc + count end)
  end
end
