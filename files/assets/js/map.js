map = new OpenLayers.Map("map");
map.addLayer(new OpenLayers.Layer.OSM());

var lonLat = new OpenLayers.LonLat(10.9689, 52.13695)
    .transform(
        new OpenLayers.Projection("EPSG:4326"), // Transformation aus dem Koordinatensystem WGS 1984
        map.getProjectionObject() // in das Koordinatensystem 'Spherical Mercator Projection'
    );

var zoom=13;

var markers = new OpenLayers.Layer.Markers("Markers");
map.addLayer(markers);

markers.addMarker(new OpenLayers.Marker(lonLat));

map.setCenter (lonLat, zoom);