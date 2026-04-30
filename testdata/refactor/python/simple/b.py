def price_with_tax_b(amount):
    rounded = round(amount, 2)
    tax = rounded * 0.085
    total = rounded + tax
    return round(total, 2)
