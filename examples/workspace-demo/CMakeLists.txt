cmake_minimum_required(VERSION 3.16)
project(workspace-demo LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(workspace-demo ${SOURCES})

