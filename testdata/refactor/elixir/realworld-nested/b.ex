defmodule Payments do
  defmodule TaxB do
    def price_with_tax(amount) do
      rounded = Float.round(amount, 2)
      tax = rounded * 0.085
      total = rounded + tax
      Float.round(total, 2)
    end
  end
end
