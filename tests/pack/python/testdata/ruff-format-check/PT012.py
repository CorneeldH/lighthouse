import pytest


def test_it():
    with pytest.raises(ValueError):
        x = int("a")
        print(x)
