def price_with_tax_a(amount):
    rounded = round(amount, 2)
    tax = rounded * 0.07
    total = rounded + tax
    return round(total, 2)
