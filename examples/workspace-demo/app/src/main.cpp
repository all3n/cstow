#include <iostream>
#include "mylib/math.hpp"
#include "stringlib/join.hpp"

int main() {
    int sum = mylib::add(3, 4);
    int prod = mylib::multiply(5, 6);

    std::string result = stringlib::join(
        std::to_string(sum),
        std::to_string(prod),
        " and "
    );

    std::cout << result << std::endl;
    return 0;
}
