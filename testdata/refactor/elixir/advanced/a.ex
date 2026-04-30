defmodule UserStore do
  def fetch_a(table, key) do
    prefix = "#{table}:"
    body = "#{prefix}#{key}"
    suffix = "/v1"
    body <> suffix
  end
end
