import asyncio
from viam.module.module import Module
try:
    from models.logging import Logging
except ModuleNotFoundError:
    # when running as local module with run.sh
    from .models.logging import Logging


if __name__ == '__main__':
    asyncio.run(Module.run_from_registry())
