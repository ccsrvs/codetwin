defmodule StockRegister do
  def total_units(bins) do
    Enum.reduce(bins, 0, fn {_code, qty}, acc -> acc + qty end)
  end

  def remove_item(bins, code, qty) do
    stored = Map.get(bins, code, 0)

    if qty > stored do
      raise ArgumentError, "cannot remove more than stored"
    end

    Map.put(bins, code, stored - qty)
  end

  def add_item(bins, limit, code, qty) do
    if qty <= 0 do
      raise ArgumentError, "count must be positive"
    end

    stored = Map.get(bins, code, 0)

    if stored + qty > limit do
      raise ArgumentError, "capacity exceeded for " <> code
    end

    Map.put(bins, code, stored + qty)
  end
end
