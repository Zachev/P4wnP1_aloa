package service

import (
	//"github.com/docker/libcontainer/netlink"
	"../netlink"
	"net"
	"log"
	"io/ioutil"
	"os"
	"fmt"

	pb "../proto"
	"errors"
)


func InitDefaultNetworkSettings() (err error) {
	//ToDo: declare managed interfaces to not check from hand
	usbEthActive,_ := CheckInterfaceExistence(USB_ETHERNET_BRIDGE_NAME)
	if usbEthActive {
		err = ConfigureInterface(GetDefaultNetworkSettingsUSB())
		if err != nil { return }
	}

	wifiEthActive, _ := CheckInterfaceExistence("wlan0")
	if wifiEthActive {
		err = ConfigureInterface(GetDefaultNetworkSettingsWiFi())
		if err != nil { return }
	}
	return
}

func ParseIPv4Mask(maskstr string) (net.IPMask, error) {
	mask := net.ParseIP(maskstr)
	if mask == nil { return nil, errors.New("Couldn't parse netmask") }

	net.ParseCIDR(maskstr)
	return net.IPv4Mask(mask[12], mask[13], mask[14], mask[15]), nil
}

func IpNetFromIPv4AndNetmask(ipv4 string, netmask string) (*net.IPNet, error) {
	mask, err := ParseIPv4Mask(netmask)
	if err != nil { return nil, err }

	ip := net.ParseIP(ipv4)
	if mask == nil { return nil, errors.New("Couldn't parse IP") }

	netw := ip.Mask(mask)

	return &net.IPNet{IP: netw, Mask: mask}, nil
}



func CreateBridge(name string) (err error) {
	return netlink.CreateBridge(name, false)
}

func setInterfaceMac(name string, mac string) error {
	return netlink.SetMacAddress(name, mac)
}

func DeleteBridge(name string) error {
	return netlink.DeleteBridge(name)
}

//Uses sysfs (not IOCTL)
func SetBridgeSTP(name string, stp_on bool) (err error) {
	value := "0"
	if (stp_on) { value = "1" }
	return ioutil.WriteFile(fmt.Sprintf("/sys/class/net/%s/bridge/stp_state", name), []byte(value), os.ModePerm)
}

func SetBridgeForwardDelay(name string, fd uint) (err error) {
	return ioutil.WriteFile(fmt.Sprintf("/sys/class/net/%s/bridge/forward_delay", name), []byte(fmt.Sprintf("%d", fd)), os.ModePerm)
}



func CheckInterfaceExistence(name string) (res bool, err error) {
	_, err = net.InterfaceByName(name)
	if err != nil {
		return false, err
	}
	return true, err
}

func NetworkLinkUp(name string) (err error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return err
	}

	err = netlink.NetworkLinkUp(iface)
	return
}

func AddInterfaceToBridgeIfExistent(bridgeName string, ifName string) (err error) {
	br, err := net.InterfaceByName(bridgeName)
	if err != nil {
		return err
	}
	iface, err := net.InterfaceByName(ifName)
	if err != nil {
		return err
	}

	err = netlink.AddToBridge(iface, br)
	if err != nil {
		return err
	}
	log.Printf("Interface %s added to bridge %s", ifName, bridgeName)
	return nil
}

func ConfigureInterface(settings *pb.EthernetInterfaceSettings) (err error) {
	//Get Interface
	iface, err := net.InterfaceByName(settings.Name)
	if err != nil {	return err }

	//stop DHCP server / client if still running
	running, _, err := IsDHCPServerRunning(settings.Name)
	if (err == nil) && running {StopDHCPServer(settings.Name)}
	running, _, err = IsDHCPClientRunning(settings.Name)
	if (err == nil) && running {StopDHCPClient(settings.Name)}

	switch settings.Mode {
	case pb.EthernetInterfaceSettings_MANUAL:
		//Generate net
		ipNet, err := IpNetFromIPv4AndNetmask(settings.IpAddress4, settings.Netmask4)
		if err != nil { return err }

		//Flush old IPs
		netlink.NetworkLinkFlush(iface)
		//set IP
		log.Printf("Setting Interface %s to IP %s\n", iface.Name, settings.IpAddress4)
		netlink.NetworkLinkAddIp(iface, net.ParseIP(settings.IpAddress4), ipNet)

		if settings.Enabled {
			log.Printf("Setting Interface %s to UP\n", iface.Name)
			err = netlink.NetworkLinkUp(iface)
		} else {
			log.Printf("Setting Interface %s to DOWN\n", iface.Name)
			err = netlink.NetworkLinkDown(iface)
		}
		if err != nil { return err }
	case pb.EthernetInterfaceSettings_DHCP_SERVER:
		//Generate net
		ipNet, err := IpNetFromIPv4AndNetmask(settings.IpAddress4, settings.Netmask4)
		if err != nil { return err }

		//Flush old IPs
		netlink.NetworkLinkFlush(iface)
		//set IP
		log.Printf("Setting Interface %s to IP %s\n", iface.Name, settings.IpAddress4)
		netlink.NetworkLinkAddIp(iface, net.ParseIP(settings.IpAddress4), ipNet)

		if settings.Enabled {
			log.Printf("Setting Interface %s to UP\n", iface.Name)
			err = netlink.NetworkLinkUp(iface)

			//check DhcpServerSettings
			if settings.DhcpServerSettings == nil {
				err = errors.New(fmt.Sprintf("Ethernet configuration for interface %s is set to DHCP Server mode, but doesn't provide DhcpServerSettings", settings.Name))
				log.Println(err)
				return err
			}
			ifName := settings.Name
			confName := NameConfigFileDHCPSrv(ifName)
			err = DHCPCreateConfigFile(settings.DhcpServerSettings, confName)
			if err != nil {return err}
			//stop already running DHCPServers for the interface
			StopDHCPServer(ifName)
			//start the DHCP server
			err = StartDHCPServer(ifName, confName)
			if err != nil {return err}
		} else {
			log.Printf("Setting Interface %s to DOWN\n", iface.Name)
			err = netlink.NetworkLinkDown(iface)
		}
		if err != nil { return err }
	case pb.EthernetInterfaceSettings_DHCP_CLIENT:
		netlink.NetworkLinkFlush(iface)
		if settings.Enabled {
			StartDHCPClient(settings.Name)
		}

	}

	return nil
}