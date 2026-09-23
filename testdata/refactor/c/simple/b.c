#include <math.h>

double price_with_tax_b(double amount)
{
    double rounded = round(amount * 100.0) / 100.0;
    double tax = rounded * 0.085;
    double total = rounded + tax;
    return round(total * 100.0) / 100.0;
}
