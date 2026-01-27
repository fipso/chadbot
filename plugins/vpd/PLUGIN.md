# VPD Plugin

Calculate Vapor Pressure Deficit (VPD) and generate VPD charts for optimal plant growing conditions.

## Skills

### calculate_vpd

Calculate the VPD given temperature and relative humidity.

**Parameters:**
- `temperature` (required): Air temperature in Celsius
- `humidity` (required): Relative humidity as percentage (0-100)
- `leaf_offset` (optional): Leaf temperature offset from air temp in Celsius (default: -2)

### vpd_chart

Generate a VPD chart as PNG image showing VPD zones with colored backgrounds.

**IMPORTANT: Always pass current_temp and current_humidity when you have sensor data!** This will draw a marker on the chart showing the current conditions.

**Parameters:**
- `min_temp` (optional): Minimum temperature in Celsius (default: 13)
- `max_temp` (optional): Maximum temperature in Celsius (default: 32)
- `min_humidity` (optional): Minimum humidity percentage (default: 20)
- `max_humidity` (optional): Maximum humidity percentage (default: 80)
- `current_temp` (required when showing current conditions): Current temperature to mark on chart
- `current_humidity` (required when showing current conditions): Current humidity to mark on chart
- `marker_label` (optional): Label for the marker (default: "Current")

**Example usage with MQTT sensor data:**
1. Get temperature from MQTT topic
2. Get humidity from MQTT topic
3. Call vpd_chart with current_temp and current_humidity set to the sensor values

### vpd_zones

Get information about VPD zones and their optimal ranges.

## VPD Zones Reference

| Zone | VPD (kPa) | Color | Description |
|------|-----------|-------|-------------|
| Danger Low | < 0.6 | Blue | Too humid - risk of mold, mildew, and fungal diseases |
| Early Veg | 0.6 - 1.0 | Deep Sky Blue | Ideal for seedlings, clones, and early vegetative growth |
| Optimal | 1.0 - 1.4 | Lime Green | Best for late veg and early flower - optimal transpiration |
| Late Flower | 1.4 - 1.8 | Gold | Good for late flowering, promotes resin production |
| Danger High | > 1.8 | Red | Too dry - risk of plant stress, nutrient lockout, stunted growth |

## Chart Orientation

The VPD chart has:
- **Y-axis (vertical)**: Temperature - low temps at top, high temps at bottom
- **X-axis (horizontal)**: Humidity - high humidity on left, low humidity on right

This makes it intuitive: moving down and right increases VPD (warmer + drier).

## VPD Formula

VPD is calculated using the Tetens formula:

```
SVP(T) = 0.6108 × exp((17.27 × T) / (T + 237.3))  [kPa]

VPD = SVP(air_temp) - SVP(air_temp) × (RH / 100)
```

Where:
- SVP = Saturated Vapor Pressure
- T = Temperature in Celsius
- RH = Relative Humidity (%)

## Tips

- **Always include current conditions on the chart** - it makes the chart much more useful
- Adjust humidity and temperature together to hit target VPD
- Use the chart to visualize which temp/humidity combinations achieve your target VPD
- The optimal zone (1.0-1.4 kPa) is where plants transpire most efficiently
