#include <iostream>
#include <gflags/gflags.h>

DEFINE_string(greeting, "Hello", "Greeting prefix");
DEFINE_string(name, "cstow", "Name to greet");
DEFINE_int32(count, 1, "Number of times to print");

int main(int argc, char* argv[]) {
    gflags::SetUsageMessage("A simple example using gflags with cstow");
    gflags::ParseCommandLineFlags(&argc, &argv, true);

    for (int i = 0; i < FLAGS_count; ++i) {
        std::cout << FLAGS_greeting << ", " << FLAGS_name << "!" << std::endl;
    }
    return 0;
}
