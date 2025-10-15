import win32evtlog
from typing import (Any, ClassVar, Dict, Final, List, Mapping, Optional,
                    Sequence, Tuple)

from typing_extensions import Self
from viam.components.sensor import *
from viam.proto.app.robot import ComponentConfig
from viam.proto.common import Geometry, ResourceName
from viam.resource.base import ResourceBase
from viam.resource.easy_resource import EasyResource
from viam.resource.types import Model, ModelFamily
from viam.utils import SensorReading, ValueTypes


class Logging(Sensor, EasyResource):
    # To enable debug-level logging, either run viam-server with the --debug option,
    # or configure your resource/machine to display debug logs.
    MODEL: ClassVar[Model] = Model(ModelFamily("jandj", "windows-logging"), "logging")

    @classmethod
    def new(
        cls, config: ComponentConfig, dependencies: Mapping[ResourceName, ResourceBase]
    ) -> Self:
        """This method creates a new instance of this Sensor component.
        The default implementation sets the name from the `config` parameter and then calls `reconfigure`.

        Args:
            config (ComponentConfig): The configuration for this resource
            dependencies (Mapping[ResourceName, ResourceBase]): The dependencies (both required and optional)

        Returns:
            Self: The resource
        """
        return super().new(config, dependencies)

    @classmethod
    def validate_config(
        cls, config: ComponentConfig
    ) -> Tuple[Sequence[str], Sequence[str]]:
        """This method allows you to validate the configuration object received from the machine,
        as well as to return any required dependencies or optional dependencies based on that `config`.

        Args:
            config (ComponentConfig): The configuration for this resource

        Returns:
            Tuple[Sequence[str], Sequence[str]]: A tuple where the
                first element is a list of required dependencies and the
                second element is a list of optional dependencies
        """
        return [], []

    def __init__(self, name: str):
        super().__init__(name)
        self.server = "localhost"
        self.log_type = "Application"
        self.num_entries = 5  # how many events to return


    def reconfigure(
        self, config: ComponentConfig, dependencies: Mapping[ResourceName, ResourceBase]
    ):
        """This method allows you to dynamically update your service when it receives a new `config` object.

        Args:
            config (ComponentConfig): The new configuration
            dependencies (Mapping[ResourceName, ResourceBase]): Any dependencies (both required and optional)
        """
        self.server = (
            config.attributes.fields.get("server").string_value or "localhost"
        )
        self.log_type = (
            config.attributes.fields.get("log_type").string_value or "Application"
        )
        self.num_entries = int(
            config.attributes.fields.get("num_entries").number_value or 5
        )
        self.logger.info(
            f"Configured Logging module for {self.server}:{self.log_type} (showing {self.num_entries} entries)"
        )
        return super().reconfigure(config, dependencies)

    async def get_readings(
        self,
        *,
        extra: Optional[Mapping[str, Any]] = None,
        timeout: Optional[float] = None,
        **kwargs,
    ) -> Mapping[str, SensorReading]:
        """
        Query recent Windows Event Log entries.
        """
        readings: Dict[str, Any] = {}
        try:
            handle = win32evtlog.OpenEventLog(self.server, self.log_type)

            flags = win32evtlog.EVENTLOG_BACKWARDS_READ | win32evtlog.EVENTLOG_SEQUENTIAL_READ
            events = win32evtlog.ReadEventLog(handle, flags, 0)

            logs = []
            for i, event in enumerate(events[: self.num_entries]):
                record = {
                    "TimeGenerated": str(event.TimeGenerated),
                    "SourceName": event.SourceName,
                    "EventID": event.EventID & 0xFFFF,
                    "EventType": event.EventType,
                    "EventCategory": event.EventCategory,
                    "Message": win32evtlog.FormatMessage(event)
                    if hasattr(win32evtlog, "FormatMessage")
                    else None,
                }
                logs.append(record)

            win32evtlog.CloseEventLog(handle)
            readings["windows_logs"] = logs
        except Exception as e:
            self.logger.error(f"Error reading Windows logs: {e}")
            readings["windows_logs"] = [{"error": str(e)}]

        return readings

    async def do_command(
        self,
        command: Mapping[str, ValueTypes],
        *,
        timeout: Optional[float] = None,
        **kwargs
    ) -> Mapping[str, ValueTypes]:
        self.logger.error("`do_command` is not implemented")
        raise NotImplementedError()

    async def get_geometries(
        self, *, extra: Optional[Dict[str, Any]] = None, timeout: Optional[float] = None
    ) -> Sequence[Geometry]:
        self.logger.error("`get_geometries` is not implemented")
        raise NotImplementedError()

