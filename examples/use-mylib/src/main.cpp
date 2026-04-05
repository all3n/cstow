#include <iostream>
#include "mylib/math.hpp"

int main() {
    std::cout << "3 + 4 = " << mylib::add(3, 4) << std::endl;
    std::cout << "5 * 6 = " << mylib::multiply(5, 6) << std::endl;
    return 0;
}
