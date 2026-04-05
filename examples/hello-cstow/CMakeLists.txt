cmake_minimum_required(VERSION 3.16)
project(hello-cstow LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(hello-cstow ${SOURCES})
target_include_directories(${PROJECT_NAME} PUBLIC include)
