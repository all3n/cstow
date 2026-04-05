cmake_minimum_required(VERSION 3.16)
project(app LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(app ${SOURCES})

# Find deps from cstow_deps
find_path(MYLIB_INCLUDE_DIR NAMES mylib/math.hpp)
find_library(MYLIB_LIBRARY NAMES mylib)
find_path(STRINGLIB_INCLUDE_DIR NAMES stringlib/join.hpp)
find_library(STRINGLIB_LIBRARY NAMES stringlib)

if(MYLIB_INCLUDE_DIR AND MYLIB_LIBRARY)
    target_include_directories(app PRIVATE ${MYLIB_INCLUDE_DIR})
    target_link_libraries(app PRIVATE ${MYLIB_LIBRARY})
else()
    target_include_directories(app PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mathlib/include)
    target_link_libraries(app PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/mathlib/lib/libmylib.a)
endif()

if(STRINGLIB_INCLUDE_DIR AND STRINGLIB_LIBRARY)
    target_include_directories(app PRIVATE ${STRINGLIB_INCLUDE_DIR})
    target_link_libraries(app PRIVATE ${STRINGLIB_LIBRARY})
else()
    target_include_directories(app PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/stringlib/include)
    target_link_libraries(app PRIVATE ${CMAKE_SOURCE_DIR}/cstow_deps/stringlib/lib/libstringlib.a)
endif()
