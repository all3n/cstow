cmake_minimum_required(VERSION 3.16)
project(mylib LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_library(mylib STATIC ${SOURCES})
target_include_directories(mylib PUBLIC include)

install(TARGETS mylib ARCHIVE DESTINATION lib)
install(DIRECTORY include/ DESTINATION include)
