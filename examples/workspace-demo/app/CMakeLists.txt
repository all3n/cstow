cmake_minimum_required(VERSION 3.16)
project(app LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

file(GLOB_RECURSE SOURCES src/*.cpp)
add_executable(app ${SOURCES})

# Dependency search paths
set(DEPS_DIR ${CMAKE_SOURCE_DIR}/cstow_deps)
file(GLOB DEP_DIRS LIST_DIRECTORIES true ${DEPS_DIR}/*)
if(DEP_DIRS)
    set(CMAKE_PREFIX_PATH ${DEP_DIRS} ${CMAKE_PREFIX_PATH})
endif()

# Find mathlib
find_path(MYLIB_INCLUDE_DIR NAMES mylib/math.hpp PATHS ${DEPS_DIR}/mathlib/include NO_DEFAULT_PATH)
find_library(MYLIB_LIBRARY NAMES mylib PATHS ${DEPS_DIR}/mathlib/build/debug ${DEPS_DIR}/mathlib NO_DEFAULT_PATH)

if(MYLIB_INCLUDE_DIR AND MYLIB_LIBRARY)
    target_include_directories(app PRIVATE ${MYLIB_INCLUDE_DIR})
    target_link_libraries(app PRIVATE ${MYLIB_LIBRARY})
else()
    message(WARNING "mathlib not found via cstow_deps, trying default search")
    find_path(MYLIB_INCLUDE_DIR NAMES mylib/math.hpp)
    find_library(MYLIB_LIBRARY NAMES mylib)
    if(MYLIB_INCLUDE_DIR AND MYLIB_LIBRARY)
        target_include_directories(app PRIVATE ${MYLIB_INCLUDE_DIR})
        target_link_libraries(app PRIVATE ${MYLIB_LIBRARY})
    endif()
endif()

# Find stringlib
find_path(STRINGLIB_INCLUDE_DIR NAMES stringlib/join.hpp PATHS ${DEPS_DIR}/stringlib/include NO_DEFAULT_PATH)
find_library(STRINGLIB_LIBRARY NAMES stringlib PATHS ${DEPS_DIR}/stringlib/build/debug ${DEPS_DIR}/stringlib NO_DEFAULT_PATH)

if(STRINGLIB_INCLUDE_DIR AND STRINGLIB_LIBRARY)
    target_include_directories(app PRIVATE ${STRINGLIB_INCLUDE_DIR})
    target_link_libraries(app PRIVATE ${STRINGLIB_LIBRARY})
else()
    message(WARNING "stringlib not found via cstow_deps, trying default search")
    find_path(STRINGLIB_INCLUDE_DIR NAMES stringlib/join.hpp)
    find_library(STRINGLIB_LIBRARY NAMES stringlib)
    if(STRINGLIB_INCLUDE_DIR AND STRINGLIB_LIBRARY)
        target_include_directories(app PRIVATE ${STRINGLIB_INCLUDE_DIR})
        target_link_libraries(app PRIVATE ${STRINGLIB_LIBRARY})
    endif()
endif()
