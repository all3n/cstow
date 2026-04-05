cmake_minimum_required(VERSION 3.16)
project(use-gflags LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(use-gflags ${SOURCES})
target_include_directories(${PROJECT_NAME} PUBLIC include)

# Find gflags from cstow_deps
find_package(gflags REQUIRED COMPONENTS static)
target_link_libraries(use-gflags PRIVATE gflags_static)
