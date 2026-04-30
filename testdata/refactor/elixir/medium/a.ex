defmodule UserFmtA do
  def format(name, age) do
    prefix = "user:"
    suffix = "(active)"
    body = "#{prefix} #{name}, age #{age}"
    body <> " " <> suffix
  end
end
