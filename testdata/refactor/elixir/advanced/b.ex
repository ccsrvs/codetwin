defmodule OrderStore do
  def fetch_b(table, key) do
    prefix = "#{table}:"
    body = "#{prefix}#{key}"
    suffix = "/v2"
    body <> suffix
  end
end
