#include "stringlib/join.hpp"

namespace stringlib {

std::string join(const std::string& a, const std::string& b, const std::string& sep) {
    return a + sep + b;
}

} // namespace stringlib
