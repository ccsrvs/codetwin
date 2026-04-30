defmodule AdminFmtB do
  def format(name, age) do
    prefix = "admin:"
    suffix = "(privileged)"
    body = "#{prefix} #{name}, age #{age}"
    body <> " " <> suffix
  end
end
