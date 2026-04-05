cmake_minimum_required(VERSION 3.16)
project(stringlib LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_library(stringlib STATIC ${SOURCES})
target_include_directories(stringlib PUBLIC include)

# Find mathlib from cstow_deps
find_path(MYLIB_INCLUDE_DIR NAMES mylib/math.hpp)
find_library(MYLIB_LIBRARY NAMES mylib)

if(MYLIB_INCLUDE_DIR AND MYLIB_LIBRARY)
    target_include_directories(stringlib PRIVATE ${MYLIB_INCLUDE_DIR})
    target_link_libraries(stringlib PRIVATE ${MYLIB_LIBRARY})
else()
    target_include_directories(stringlib PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mathlib/include)
    target_link_libraries(stringlib PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mathlib/lib/libmylib.a)
endif()

install(TARGETS stringlib ARCHIVE DESTINATION lib)
install(DIRECTORY include/ DESTINATION include)
